package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
	"github.com/boltdb/bolt"
)

type termInfo struct {
	frequency int
	positions []int
}

func (e *SearchEngine) IndexFile(r io.Reader) (int, string, error) {
	decoder := json.NewDecoder(r)
	var indexed, skipped, dupSkipped int

	for {
		var doc Document
		if err := decoder.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return indexed + skipped, "", fmt.Errorf("JSON 解析错误: %w", err)
		}
		if doc.Title == "" && doc.Content == "" {
			skipped++
			continue
		}
		added, err := e.indexOne(doc)
		if err != nil {
			return indexed + skipped, "", fmt.Errorf("索引第 %d 篇文档失败: %w", indexed+1, err)
		}
		if added == 0 {
			dupSkipped++
		} else {
			indexed++
		}
	}

	summary := ""
	var parts []string
	if indexed > 0 {
		parts = append(parts, fmt.Sprintf("新增 %d 篇", indexed))
	}
	if dupSkipped > 0 {
		parts = append(parts, fmt.Sprintf("跳过 %d 篇重复", dupSkipped))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("跳过 %d 条空文档", skipped))
	}
	if len(parts) > 0 {
		summary = "（" + fmt.Sprintf("%s", parts[0])
		for _, p := range parts[1:] {
			summary += "，" + p
		}
		summary += "）"
	}

	return indexed, summary, nil
}

func (e *SearchEngine) AddDocument(title, content string) (uint64, error) {
	return e.indexOne(Document{Title: title, Content: content})
}

func docHash(title, content string) []byte {
	h := sha256.Sum256([]byte(title + "\x00" + content))
	return h[:8]
}

func (e *SearchEngine) indexOne(doc Document) (uint64, error) {
	hash := docHash(doc.Title, doc.Content)

	tokens := e.tokenizer.Tokenize(doc.Title + " " + doc.Content)

	termStats := make(map[string]termInfo)
	for _, t := range tokens {
		info := termStats[t.Term]
		info.frequency++
		info.positions = append(info.positions, t.Position)
		termStats[t.Term] = info
	}

	var docID uint64
	var newTerms []string
	err := e.db.Update(func(tx *bolt.Tx) error {
		hb := tx.Bucket([]byte(bucketHash))
		if hb.Get(hash) != nil {
			return nil
		}

		meta, err := e.getMeta(tx)
		if err != nil {
			return err
		}
		docID = meta.NextDocID
		meta.NextDocID++
		meta.TotalDocs++
		meta.TotalTokens += uint64(len(tokens))

		doc.ID = docID
		docData, err := msgpack.Marshal(doc)
		if err != nil {
			return fmt.Errorf("序列化文档失败: %w", err)
		}
		if err := tx.Bucket([]byte(bucketDocs)).Put(encUint64(docID), docData); err != nil {
			return fmt.Errorf("存储文档失败: %w", err)
		}

		if err := tx.Bucket([]byte(bucketDocLen)).Put(encUint64(docID), encUint64(uint64(len(tokens)))); err != nil {
			return fmt.Errorf("存储文档长度失败: %w", err)
		}

		if err := hb.Put(hash, encUint64(docID)); err != nil {
			return fmt.Errorf("存储去重哈希失败: %w", err)
		}

		idxBucket := tx.Bucket([]byte(bucketIndex))
		dfBucket := tx.Bucket([]byte(bucketDF))

		for term, info := range termStats {
			plData := idxBucket.Get([]byte(term))
			var pl PostingList
			if plData != nil {
				if err := msgpack.Unmarshal(plData, &pl); err != nil {
					return fmt.Errorf("反序列化 posting list 失败: %w", err)
				}
			}
			pl.Postings = append(pl.Postings, Posting{
				DocID:     docID,
				TF:        info.frequency,
				Positions: info.positions,
			})
			newPLData, err := msgpack.Marshal(pl)
			if err != nil {
				return fmt.Errorf("序列化 posting list 失败: %w", err)
			}
			if err := idxBucket.Put([]byte(term), newPLData); err != nil {
				return fmt.Errorf("更新倒排索引失败: %w", err)
			}

			dfData := dfBucket.Get([]byte(term))
			if dfData == nil {
				newTerms = append(newTerms, term)
			}
			var df uint64
			if dfData != nil {
				df = decUint64(dfData)
			}
			df++
			if err := dfBucket.Put([]byte(term), encUint64(df)); err != nil {
				return fmt.Errorf("更新文档频率失败: %w", err)
			}
		}

		return e.setMeta(tx, meta)
	})
	if err != nil {
		return 0, err
	}

	if len(newTerms) > 0 {
		e.mu.Lock()
		e.ensureBKTree()
		for _, t := range newTerms {
			e.bkTree.insert(t)
		}
		e.mu.Unlock()
	}

	return docID, nil
}
