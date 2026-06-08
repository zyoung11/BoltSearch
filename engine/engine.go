package engine

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
	"github.com/boltdb/bolt"
)

const (
	bucketDocs   = "docs"
	bucketIndex  = "index"
	bucketMeta   = "meta"
	bucketDocLen = "doclen"
	bucketDF     = "df"
	bucketHash   = "hash"
	metaKeyRoot  = "root"
)

type SearchEngine struct {
	db        *bolt.DB
	dbPath    string
	tokenizer *Tokenizer
	bkTree    *bkNode
	bkBuilt   bool
	mu        sync.Mutex
}

func encUint64(v uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	return buf
}

func decUint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

func NewSearchEngine(dbPath string) (*SearchEngine, error) {
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 0})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	tokenizer, err := NewTokenizer()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化分词器失败: %w", err)
	}

	engine := &SearchEngine{
		db:        db,
		dbPath:    dbPath,
		tokenizer: tokenizer,
	}

	if err := engine.initBuckets(); err != nil {
		db.Close()
		return nil, err
	}

	if err := engine.initMeta(); err != nil {
		db.Close()
		return nil, err
	}

	return engine, nil
}

func (e *SearchEngine) Close() error {
	return e.db.Close()
}

func (e *SearchEngine) initBuckets() error {
	buckets := []string{bucketDocs, bucketIndex, bucketMeta, bucketDocLen, bucketDF, bucketHash}
	return e.db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("创建 Bucket %s 失败: %w", name, err)
			}
		}
		return nil
	})
}

func (e *SearchEngine) initMeta() error {
	return e.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMeta))
		if b.Get([]byte(metaKeyRoot)) != nil {
			return nil
		}
		meta := IndexMeta{NextDocID: 1}
		data, err := msgpack.Marshal(meta)
		if err != nil {
			return fmt.Errorf("序列化元数据失败: %w", err)
		}
		return b.Put([]byte(metaKeyRoot), data)
	})
}

func (e *SearchEngine) getMeta(tx *bolt.Tx) (*IndexMeta, error) {
	b := tx.Bucket([]byte(bucketMeta))
	data := b.Get([]byte(metaKeyRoot))
	if data == nil {
		return nil, fmt.Errorf("元数据不存在")
	}
	var meta IndexMeta
	if err := msgpack.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("反序列化元数据失败: %w", err)
	}
	return &meta, nil
}

func (e *SearchEngine) setMeta(tx *bolt.Tx, meta *IndexMeta) error {
	b := tx.Bucket([]byte(bucketMeta))
	data, err := msgpack.Marshal(meta)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}
	return b.Put([]byte(metaKeyRoot), data)
}
