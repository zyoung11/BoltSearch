package engine

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/vmihailenco/msgpack/v5"
	"github.com/boltdb/bolt"
)

func (e *SearchEngine) Search(query string, mode string, limit int, offset int, prefix bool, fuzzy bool) ([]ScoredDoc, int, error) {
	tokens := e.tokenizer.Tokenize(query)
	if len(tokens) == 0 {
		return nil, 0, nil
	}

	uniqueTerms := make(map[string]bool)
	var queryTerms []string
	for _, t := range tokens {
		if !uniqueTerms[t.Term] {
			uniqueTerms[t.Term] = true
			queryTerms = append(queryTerms, t.Term)
		}
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

		for _, term := range queryTerms {
			var allPostings []Posting
			termIDF := idf(N, 0)

			collect := func(t string) bool {
				var found bool
				if prefix {
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

			if !collect(term) && fuzzy && !prefix {
				e.buildBKTree()
				if e.bkTree != nil {
					candidates := e.bkTree.search(term, 2)
					for _, c := range candidates {
						if c != term {
							collect(c)
						}
					}
				}
			}

			for _, posting := range allPostings {
				docLenData := docLenBucket.Get(encUint64(posting.DocID))
				var docLen uint64
				if docLenData != nil {
					docLen = decUint64(docLenData)
				}

				score := bm25Score(posting.TF, docLen, avgdl, termIDF)

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
		if mode == "and" && len(matchedTerms) < len(queryTerms) {
			continue
		}
		filtered = append(filtered, sd)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if math.Abs(filtered[i].Score-filtered[j].Score) < 0.0001 {
			return filtered[i].Doc.ID < filtered[j].Doc.ID
		}
		return filtered[i].Score > filtered[j].Score
	})

	totalHits := len(filtered)

	if offset >= len(filtered) {
		return nil, totalHits, nil
	}
	end := min(offset+limit, len(filtered))
	results := filtered[offset:end]

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

func (e *SearchEngine) DeleteDocument(docID uint64) error {
	return e.db.Update(func(tx *bolt.Tx) error {
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

	var suggestions []string
	err := e.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketIndex))
		c := b.Cursor()
		for k, _ := c.Seek([]byte(searchPrefix)); k != nil && strings.HasPrefix(string(k), searchPrefix); k, _ = c.Next() {
			suggestions = append(suggestions, string(k))
			if limit > 0 && len(suggestions) >= limit {
				break
			}
		}
		return nil
	})
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
			headers = []string{"DocID", "Title", "Content"}
			return b.ForEach(func(k, v []byte) error {
				id := decUint64(k)
				var doc Document
				msgpack.Unmarshal(v, &doc)
				rows = append(rows, []string{
					fmt.Sprintf("%d", id),
					doc.Title,
					doc.Content,
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
