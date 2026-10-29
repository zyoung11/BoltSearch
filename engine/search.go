package engine

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vmihailenco/msgpack/v5"
	bolt "go.etcd.io/bbolt"
)

const (
	fuzzyWeight   = 0.35
	fuzzyMaxTerms = 4
)

// PreparedQuery 保存一次解析完成的查询，包含去重后的查询词与模糊候选。
type PreparedQuery struct {
	Query string
	Terms []string
	Fuzzy map[string][]string
}

// Prepare 对查询做分词和模糊候选解析，结果可复用于多次 SearchPrepared 调用。
func (e *SearchEngine) Prepare(query string) *PreparedQuery {
	pq := &PreparedQuery{Query: query, Fuzzy: make(map[string][]string)}
	seen := make(map[string]bool)
	for _, t := range e.tokenizer.Tokenize(query) {
		if !seen[t.Term] {
			seen[t.Term] = true
			pq.Terms = append(pq.Terms, t.Term)
		}
	}
	if len(pq.Terms) == 0 {
		return pq
	}
	e.ensureBKTree()
	if e.bkTree == nil {
		return pq
	}
	for _, term := range pq.Terms {
		maxDist := 2
		if utf8.RuneCountInString(term) <= 2 {
			maxDist = 1
		}
		candidates := e.bkTree.search(term, maxDist)
		sort.Strings(candidates)
		list := make([]string, 0, len(candidates))
		for _, c := range candidates {
			if c != term {
				list = append(list, c)
			}
		}
		pq.Fuzzy[term] = list
	}
	return pq
}

// QueryTerms 返回查询分词去重后的词项，供上层做命中高亮。
func (e *SearchEngine) QueryTerms(query string) []string {
	var terms []string
	seen := make(map[string]bool)
	for _, t := range e.tokenizer.Tokenize(query) {
		if !seen[t.Term] {
			seen[t.Term] = true
			terms = append(terms, t.Term)
		}
	}
	return terms
}

// Search 解析查询并执行一次搜索，返回结果页与命中总数。
func (e *SearchEngine) Search(query string, opts SearchOptions) ([]ScoredDoc, int, error) {
	return e.SearchPrepared(e.Prepare(query), opts)
}

// SearchPrepared 用预备好的查询执行搜索，返回结果页与命中总数。
func (e *SearchEngine) SearchPrepared(pq *PreparedQuery, opts SearchOptions) ([]ScoredDoc, int, error) {
	if len(pq.Terms) == 0 {
		return nil, 0, nil
	}

	scoredDocs := make(map[uint64]*ScoredDoc)
	var avgdl float64

	err := e.db.View(func(tx *bolt.Tx) error {
		meta, err := e.getMeta(tx)
		if err != nil {
			return err
		}
		if meta.TotalDocs == 0 {
			return nil
		}
		avgdl = float64(meta.TotalTokens) / float64(meta.TotalDocs)
		N := meta.TotalDocs

		idxBucket := tx.Bucket([]byte(bucketIndex))
		dfBucket := tx.Bucket([]byte(bucketDF))
		docLenBucket := tx.Bucket([]byte(bucketDocLen))
		docBucket := tx.Bucket([]byte(bucketDocs))

		filterCache := make(map[uint64]bool)
		passFilter := func(docID uint64) bool {
			if len(opts.MetaFilter) == 0 {
				return true
			}
			if ok, hit := filterCache[docID]; hit {
				return ok
			}
			ok := false
			var doc Document
			if data := docBucket.Get(encUint64(docID)); data != nil && msgpack.Unmarshal(data, &doc) == nil {
				ok = matchMeta(doc.Meta, opts.MetaFilter)
			}
			filterCache[docID] = ok
			return ok
		}

		for _, term := range pq.Terms {
			var allPostings []Posting
			termIDF := idf(N, 0)
			weight := 1.0

			collect := func(t string) bool {
				var found bool
				if opts.Prefix {
					c := idxBucket.Cursor()
					for k, v := c.Seek([]byte(t)); k != nil && strings.HasPrefix(string(k), t); k, v = c.Next() {
						var pl PostingList
						if err := msgpack.Unmarshal(v, &pl); err != nil {
							continue
						}
						allPostings = append(allPostings, pl.Postings...)
						found = true
					}
				} else {
					plData := idxBucket.Get([]byte(t))
					if plData == nil {
						return false
					}
					var pl PostingList
					if err := msgpack.Unmarshal(plData, &pl); err != nil {
						return false
					}
					allPostings = append(allPostings, pl.Postings...)
					found = true
				}
				if found {
					dfData := dfBucket.Get([]byte(t))
					var nDocWithTerm uint64
					if dfData != nil {
						nDocWithTerm = decUint64(dfData)
					}
					termIDF = idf(N, nDocWithTerm)
				}
				return found
			}

			if !collect(term) && opts.Fuzzy {
				collected := 0
				for _, c := range pq.Fuzzy[term] {
					if collect(c) {
						weight = fuzzyWeight
						collected++
						if collected >= fuzzyMaxTerms {
							break
						}
					}
				}
			}

			for _, posting := range allPostings {
				if !passFilter(posting.DocID) {
					continue
				}
				docLenData := docLenBucket.Get(encUint64(posting.DocID))
				var docLen uint64
				if docLenData != nil {
					docLen = decUint64(docLenData)
				}

				score := bm25Score(posting.TF, docLen, avgdl, termIDF) * weight

				sd, exists := scoredDocs[posting.DocID]
				if !exists {
					sd = &ScoredDoc{
						Doc:        Document{ID: posting.DocID},
						MatchedOn:  []string{},
						MatchCount: 0,
					}
					scoredDocs[posting.DocID] = sd
				}
				sd.Score += score
				sd.MatchedOn = append(sd.MatchedOn, term)
				sd.MatchCount += posting.TF
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	var filtered []*ScoredDoc
	for _, sd := range scoredDocs {
		matchedTerms := make(map[string]bool)
		for _, t := range sd.MatchedOn {
			matchedTerms[t] = true
		}
		if opts.Mode == "and" && len(matchedTerms) < len(pq.Terms) {
			continue
		}
		filtered = append(filtered, sd)
	}

	if opts.Phrase {
		filtered = e.applyPhraseBonus(pq.Query, filtered, opts)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if math.Abs(filtered[i].Score-filtered[j].Score) < 0.0001 {
			return filtered[i].Doc.ID < filtered[j].Doc.ID
		}
		return filtered[i].Score > filtered[j].Score
	})

	totalHits := len(filtered)

	if opts.Offset >= len(filtered) {
		return nil, totalHits, nil
	}
	end := min(opts.Offset+opts.Limit, len(filtered))
	results := filtered[opts.Offset:end]

	for _, sd := range results {
		if err := e.db.View(func(tx *bolt.Tx) error {
			docData := tx.Bucket([]byte(bucketDocs)).Get(encUint64(sd.Doc.ID))
			if docData == nil {
				return fmt.Errorf("文档 %d 不存在", sd.Doc.ID)
			}
			return msgpack.Unmarshal(docData, &sd.Doc)
		}); err != nil {
			return nil, totalHits, err
		}
	}

	docs := make([]ScoredDoc, len(results))
	for i, sd := range results {
		docs[i] = *sd
	}
	return docs, totalHits, nil
}

// applyPhraseBonus 把短语通道加分合并进词项结果，or 模式下补入纯短语命中。
func (e *SearchEngine) applyPhraseBonus(query string, scored []*ScoredDoc, opts SearchOptions) []*ScoredDoc {
	index := e.ensurePhraseIndex()
	if index == nil {
		return scored
	}
	bonus := index.bonuses(query)
	for _, sd := range scored {
		if extra, ok := bonus[sd.Doc.ID]; ok {
			sd.Score += extra
			delete(bonus, sd.Doc.ID)
		}
	}
	if opts.Mode == "and" {
		return scored
	}
	for _, entry := range index.entries {
		extra, ok := bonus[entry.doc.ID]
		if !ok {
			continue
		}
		if !matchMeta(entry.doc.Meta, opts.MetaFilter) {
			continue
		}
		scored = append(scored, &ScoredDoc{Doc: entry.doc, Score: extra})
	}
	return scored
}

func (e *SearchEngine) DeleteDocument(docID uint64) error {
	var removed int
	err := e.db.Update(func(tx *bolt.Tx) error {
		docBucket := tx.Bucket([]byte(bucketDocs))
		docData := docBucket.Get(encUint64(docID))
		if docData == nil {
			return fmt.Errorf("文档 %d 不存在", docID)
		}
		var doc Document
		if err := msgpack.Unmarshal(docData, &doc); err != nil {
			return fmt.Errorf("反序列化文档失败: %w", err)
		}

		tokens := e.tokenizer.Tokenize(doc.Title + " " + doc.Content)
		termSet := make(map[string]bool)
		for _, t := range tokens {
			termSet[t.Term] = true
		}

		docLenData := tx.Bucket([]byte(bucketDocLen)).Get(encUint64(docID))
		var docLen uint64
		if docLenData != nil {
			docLen = decUint64(docLenData)
		}

		idxBucket := tx.Bucket([]byte(bucketIndex))
		dfBucket := tx.Bucket([]byte(bucketDF))

		for term := range termSet {
			plData := idxBucket.Get([]byte(term))
			if plData == nil {
				continue
			}
			var pl PostingList
			if err := msgpack.Unmarshal(plData, &pl); err != nil {
				continue
			}
			newPostings := make([]Posting, 0, len(pl.Postings))
			for _, p := range pl.Postings {
				if p.DocID != docID {
					newPostings = append(newPostings, p)
				}
			}
			if len(newPostings) == 0 {
				if err := idxBucket.Delete([]byte(term)); err != nil {
					return fmt.Errorf("删除倒排索引条目失败: %w", err)
				}
			} else {
				newPLData, err := msgpack.Marshal(PostingList{Postings: newPostings})
				if err != nil {
					return fmt.Errorf("序列化 posting list 失败: %w", err)
				}
				if err := idxBucket.Put([]byte(term), newPLData); err != nil {
					return fmt.Errorf("更新倒排索引失败: %w", err)
				}
			}

			dfData := dfBucket.Get([]byte(term))
			if dfData != nil {
				df := decUint64(dfData)
				if df <= 1 {
					dfBucket.Delete([]byte(term))
					removed++
				} else {
					dfBucket.Put([]byte(term), encUint64(df-1))
				}
			}
		}

		docBucket.Delete(encUint64(docID))
		tx.Bucket([]byte(bucketDocLen)).Delete(encUint64(docID))

		hash := docHash(doc.Title, doc.Content)
		tx.Bucket([]byte(bucketHash)).Delete(hash)

		meta, err := e.getMeta(tx)
		if err != nil {
			return err
		}
		meta.TotalDocs--
		meta.TotalTokens -= docLen
		return e.setMeta(tx, meta)
	})
	if err == nil {
		e.mu.Lock()
		e.phraseIdx = nil
		e.phraseBuilt = false
		if removed > 0 {
			e.bkBuilt = false
		}
		e.mu.Unlock()
	}
	return err
}

func (e *SearchEngine) Stats() (Stats, error) {
	var s Stats
	err := e.db.View(func(tx *bolt.Tx) error {
		meta, err := e.getMeta(tx)
		if err != nil {
			return err
		}
		s.TotalDocs = meta.TotalDocs
		s.TotalTokens = meta.TotalTokens
		if meta.TotalDocs > 0 {
			s.AvgDocLen = float64(meta.TotalTokens) / float64(meta.TotalDocs)
		}
		s.UniqueTerms = tx.Bucket([]byte(bucketDF)).Stats().KeyN
		return nil
	})
	if err != nil {
		return s, err
	}
	fi, err := os.Stat(e.dbPath)
	if err == nil {
		s.DBFileSize = fi.Size()
	}
	return s, nil
}

func (e *SearchEngine) BucketCounts() map[string]int {
	counts := make(map[string]int)
	e.db.View(func(tx *bolt.Tx) error {
		for _, name := range []string{bucketDocs, bucketIndex, bucketMeta, bucketDocLen, bucketDF, bucketHash} {
			b := tx.Bucket([]byte(name))
			if b != nil {
				counts[name] = b.Stats().KeyN
			}
		}
		return nil
	})
	return counts
}

func (e *SearchEngine) Suggest(prefix string, limit int) ([]string, error) {
	tokens := e.tokenizer.Tokenize(prefix)
	searchPrefix := prefix
	if len(tokens) > 0 {
		searchPrefix = tokens[0].Term
	}

	type candidate struct {
		term string
		df   uint64
	}
	var candidates []candidate
	err := e.db.View(func(tx *bolt.Tx) error {
		idxBucket := tx.Bucket([]byte(bucketIndex))
		dfBucket := tx.Bucket([]byte(bucketDF))
		c := idxBucket.Cursor()
		for k, _ := c.Seek([]byte(searchPrefix)); k != nil && strings.HasPrefix(string(k), searchPrefix); k, _ = c.Next() {
			term := string(k)
			var df uint64
			if d := dfBucket.Get(k); d != nil {
				df = decUint64(d)
			}
			candidates = append(candidates, candidate{term, df})
			if limit > 0 && len(candidates) >= limit*3 {
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].df > candidates[j].df })

	var suggestions []string
	for i, c := range candidates {
		if limit > 0 && i >= limit {
			break
		}
		suggestions = append(suggestions, c.term)
	}
	return suggestions, err
}

func (e *SearchEngine) ScanBucket(bucketName string) ([]string, [][]string, error) {
	var headers []string
	var rows [][]string

	err := e.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %s 不存在", bucketName)
		}

		switch bucketName {
		case bucketDocs:
			headers = []string{"DocID", "Title", "Content", "Meta"}
			return b.ForEach(func(k, v []byte) error {
				id := decUint64(k)
				var doc Document
				msgpack.Unmarshal(v, &doc)
				rows = append(rows, []string{
					fmt.Sprintf("%d", id),
					doc.Title,
					doc.Content,
					formatMeta(doc.Meta),
				})
				return nil
			})

		case bucketIndex:
			headers = []string{"Term", "Docs", "DocIDs"}
			return b.ForEach(func(k, v []byte) error {
				var pl PostingList
				msgpack.Unmarshal(v, &pl)
				docIDs := make([]string, len(pl.Postings))
				for i, p := range pl.Postings {
					docIDs[i] = fmt.Sprintf("%d", p.DocID)
				}
				rows = append(rows, []string{
					string(k),
					fmt.Sprintf("%d", len(pl.Postings)),
					strings.Join(docIDs, ","),
				})
				return nil
			})

		case bucketMeta:
			headers = []string{"Key", "NextDocID", "TotalDocs", "TotalTokens"}
			return b.ForEach(func(k, v []byte) error {
				var meta IndexMeta
				msgpack.Unmarshal(v, &meta)
				rows = append(rows, []string{
					string(k),
					fmt.Sprintf("%d", meta.NextDocID),
					fmt.Sprintf("%d", meta.TotalDocs),
					fmt.Sprintf("%d", meta.TotalTokens),
				})
				return nil
			})

		case bucketDocLen:
			headers = []string{"DocID", "Tokens"}
			return b.ForEach(func(k, v []byte) error {
				id := decUint64(k)
				tokens := decUint64(v)
				rows = append(rows, []string{
					fmt.Sprintf("%d", id),
					fmt.Sprintf("%d", tokens),
				})
				return nil
			})

		case bucketDF:
			headers = []string{"Term", "DF"}
			return b.ForEach(func(k, v []byte) error {
				df := decUint64(v)
				rows = append(rows, []string{
					string(k),
					fmt.Sprintf("%d", df),
				})
				return nil
			})

		case bucketHash:
			headers = []string{"Hash", "DocID"}
			return b.ForEach(func(k, v []byte) error {
				id := decUint64(v)
				rows = append(rows, []string{
					fmt.Sprintf("%x", k),
					fmt.Sprintf("%d", id),
				})
				return nil
			})

		default:
			return fmt.Errorf("未知 bucket: %s", bucketName)
		}
	})

	return headers, rows, err
}

func (e *SearchEngine) buildBKTree() {
	if e.bkBuilt {
		return
	}
	e.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDF))
		if b == nil {
			return nil
		}
		first := true
		return b.ForEach(func(k, v []byte) error {
			term := string(k)
			if first {
				e.bkTree = &bkNode{term: term}
				first = false
			} else {
				e.bkTree.insert(term)
			}
			return nil
		})
	})
	e.bkBuilt = true
}

func (e *SearchEngine) ensureBKTree() {
	if !e.bkBuilt {
		e.buildBKTree()
	}
}

// ensurePhraseIndex 返回短语索引，首次使用时构建，文档变更后重建。
func (e *SearchEngine) ensurePhraseIndex() *phraseIndex {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.phraseBuilt {
		return e.phraseIdx
	}
	docs, err := e.AllDocuments()
	if err != nil {
		return nil
	}
	e.phraseIdx = newPhraseIndex(docs)
	e.phraseBuilt = true
	return e.phraseIdx
}

// AllDocuments 返回全部已索引文档，供上层做原文比对。
func (e *SearchEngine) AllDocuments() ([]Document, error) {
	var docs []Document
	err := e.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketDocs)).ForEach(func(k, v []byte) error {
			var doc Document
			if err := msgpack.Unmarshal(v, &doc); err != nil {
				return err
			}
			doc.ID = decUint64(k)
			docs = append(docs, doc)
			return nil
		})
	})
	return docs, err
}

// WarmUp 提前构建 BK-Tree，把惰性构建成本从首个查询挪到调用时刻。
func (e *SearchEngine) WarmUp() {
	e.ensureBKTree()
}
