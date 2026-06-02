package boltLib

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"

	bolt "github.com/boltdb/bolt"
)

type KeyType string

const (
	KeyTypeString KeyType = "string"
	KeyTypeSeq    KeyType = "seq"
	KeyTypeTime   KeyType = "time"
)

var (
	ErrKeyNotFound    = errors.New("key not found")
	ErrBucketNotFound = errors.New("bucket not found")
)

const (
	metadataBucket = "BoltbaseMetaDataForBucketsKeyType"
	layoutMicro    = "2006-01-02T15:04:05.000000Z07:00"
)

type BucketInfo struct {
	Name    string `json:"name"`
	KeyType string `json:"keyType"`
	Count   int    `json:"count"`
}

func withDB(dbPath string, fn func(*bolt.DB) error) error {
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := initMetadata(db); err != nil {
		return err
	}
	return fn(db)
}

func withDB1[T any](dbPath string, fn func(*bolt.DB) (T, error)) (T, error) {
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		var zero T
		return zero, err
	}
	defer db.Close()
	if err := initMetadata(db); err != nil {
		var zero T
		return zero, err
	}
	return fn(db)
}

func initMetadata(db *bolt.DB) error {
	var list []string
	if err := db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
			list = append(list, string(name))
			return nil
		})
	}); err != nil {
		return err
	}
	if slices.Contains(list, metadataBucket) {
		return nil
	}
	return db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte(metadataBucket))
		return err
	})
}

// getKeyType retrieves the stored KeyType for a bucket.
func getKeyType(db *bolt.DB, bucket string) (string, error) {
	var kt string
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(metadataBucket))
		if b == nil {
			return ErrBucketNotFound
		}
		v := b.Get([]byte(bucket))
		if v == nil {
			return errors.New("bucket '" + bucket + "' not found or has no key type")
		}
		kt = string(v)
		return nil
	})
	return kt, err
}

// uint32ToPadded10BE converts 4 big-endian bytes to a zero-padded 10-digit string.
func uint32ToPadded10BE(b []byte) string {
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])

	hi := v / 100_000_000
	lo := v - hi*100_000_000

	lo64 := uint64(lo)

	const inv10 = 0xCCCCCCCD
	const shift = 35

	q0 := (lo64 * inv10) >> shift
	d9 := lo64 - q0*10
	q1 := (q0 * inv10) >> shift
	d8 := q0 - q1*10
	q2 := (q1 * inv10) >> shift
	d7 := q1 - q2*10
	q3 := (q2 * inv10) >> shift
	d6 := q2 - q3*10
	q4 := (q3 * inv10) >> shift
	d5 := q3 - q4*10
	q5 := (q4 * inv10) >> shift
	d4 := q4 - q5*10
	q6 := (q5 * inv10) >> shift
	d3 := q5 - q6*10
	q7 := (q6 * inv10) >> shift
	d2 := q6 - q7*10

	hi10 := (uint64(hi) * inv10) >> shift
	d1 := uint64(hi) - hi10*10
	d0 := hi10

	var buf [10]byte
	buf[0] = byte(d0) + '0'
	buf[1] = byte(d1) + '0'
	buf[2] = byte(d2) + '0'
	buf[3] = byte(d3) + '0'
	buf[4] = byte(d4) + '0'
	buf[5] = byte(d5) + '0'
	buf[6] = byte(d6) + '0'
	buf[7] = byte(d7) + '0'
	buf[8] = byte(d8) + '0'
	buf[9] = byte(d9) + '0'

	return string(buf[:])
}

// CreateBucket creates a new bucket with the given key type.
func CreateBucket(dbPath, name string, keyType KeyType) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		if err := db.Update(func(tx *bolt.Tx) error {
			if tx.Bucket([]byte(name)) != nil {
				return errors.New("bucket already exists")
			}
			_, err := tx.CreateBucket([]byte(name))
			return err
		}); err != nil {
			return err
		}
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(metadataBucket))
			return b.Put([]byte(name), []byte(string(keyType)))
		})
	})
}

// ListBuckets returns all buckets except the internal metadata bucket.
func ListBuckets(dbPath string) ([]BucketInfo, error) {
	return withDB1(dbPath, func(db *bolt.DB) ([]BucketInfo, error) {
		var result []BucketInfo
		err := db.View(func(tx *bolt.Tx) error {
			return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
				sname := string(name)
				if sname == metadataBucket {
					return nil
				}
				mb := tx.Bucket([]byte(metadataBucket))
				kt := "string"
				if mb != nil {
					if v := mb.Get(name); v != nil {
						kt = string(v)
					}
				}
				result = append(result, BucketInfo{
					Name:    sname,
					KeyType: kt,
					Count:   b.Stats().KeyN,
				})
				return nil
			})
		})
		return result, err
	})
}

// RenameBucket renames a bucket and updates the metadata key.
func RenameBucket(dbPath, oldName, newName string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		if oldName == metadataBucket || newName == metadataBucket {
			return errors.New("cannot rename internal metadata bucket")
		}
		kt, err := getKeyType(db, oldName)
		if err != nil {
			return err
		}
		if err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(oldName))
			if b == nil {
				return ErrBucketNotFound
			}
			if tx.Bucket([]byte(newName)) != nil {
				return errors.New("bucket already exists")
			}
			nb, err := tx.CreateBucket([]byte(newName))
			if err != nil {
				return err
			}
			if err := b.ForEach(func(k, v []byte) error {
				return nb.Put(k, v)
			}); err != nil {
				return err
			}
			return tx.DeleteBucket([]byte(oldName))
		}); err != nil {
			return err
		}
		return db.Update(func(tx *bolt.Tx) error {
			mb := tx.Bucket([]byte(metadataBucket))
			mb.Delete([]byte(oldName))
			return mb.Put([]byte(newName), []byte(kt))
		})
	})
}

// DropBucket deletes a bucket and its metadata entry.
func DropBucket(dbPath, name string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		if name == metadataBucket {
			return errors.New("cannot drop internal metadata bucket")
		}
		if err := db.Update(func(tx *bolt.Tx) error {
			if tx.Bucket([]byte(name)) == nil {
				return ErrBucketNotFound
			}
			return tx.DeleteBucket([]byte(name))
		}); err != nil {
			return err
		}
		return db.Update(func(tx *bolt.Tx) error {
			mb := tx.Bucket([]byte(metadataBucket))
			return mb.Delete([]byte(name))
		})
	})
}

// Put inserts or updates a key-value pair in a string-type bucket.
func Put(dbPath, bucket, key, value string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return err
		}
		if kt != string(KeyTypeString) {
			return errors.New("cannot use Put on " + kt + "-type bucket; use PutSeq or PutTime")
		}
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			return b.Put([]byte(key), []byte(value))
		})
	})
}

// PutIfNotExists inserts a key-value pair only if the key does not already exist.
func PutIfNotExists(dbPath, bucket, key, value string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return err
		}
		if kt != string(KeyTypeString) {
			return errors.New("cannot use PutIfNotExists on " + kt + "-type bucket")
		}
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			if b.Get([]byte(key)) != nil {
				return errors.New("key already exists")
			}
			return b.Put([]byte(key), []byte(value))
		})
	})
}

// PutSeq inserts a value with an auto-increment uint32 key into a seq-type bucket.
// Returns the auto-generated key.
func PutSeq(dbPath, bucket, value string) (uint64, error) {
	return withDB1(dbPath, func(db *bolt.DB) (uint64, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return 0, err
		}
		if kt != string(KeyTypeSeq) {
			return 0, errors.New("cannot use PutSeq on " + kt + "-type bucket")
		}
		var id uint64
		err = db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			b.FillPercent = 0.95
			id, err = b.NextSequence()
			if err != nil {
				return err
			}
			key := make([]byte, 4)
			binary.BigEndian.PutUint32(key, uint32(id))
			return b.Put(key, []byte(value))
		})
		return id, err
	})
}

// PutTime inserts a value with a microsecond-precision timestamp key into a
// time-type bucket. Returns the generated time key string.
func PutTime(dbPath, bucket, value string) (string, error) {
	return withDB1(dbPath, func(db *bolt.DB) (string, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return "", err
		}
		if kt != string(KeyTypeTime) {
			return "", errors.New("cannot use PutTime on " + kt + "-type bucket")
		}
		var key string
		err = db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			b.FillPercent = 0.95
			k := []byte(time.Now().UTC().Format(layoutMicro))
			key = string(k)
			return b.Put(k, []byte(value))
		})
		return key, err
	})
}

// Get retrieves a value by key. For seq-type buckets, key must be a
// string representation of the uint32 ID.
func Get(dbPath, bucket, key string) (string, error) {
	return withDB1(dbPath, func(db *bolt.DB) (string, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return "", err
		}
		var value string
		err = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			var k []byte
			if kt == string(KeyTypeSeq) {
				var id uint64
				id, err = parseUint64(key)
				if err != nil {
					return errors.New("key must be an unsigned integer for seq-type bucket")
				}
				k = make([]byte, 4)
				binary.BigEndian.PutUint32(k, uint32(id))
			} else {
				k = []byte(key)
			}
			v := b.Get(k)
			if v == nil {
				return ErrKeyNotFound
			}
			value = string(v)
			return nil
		})
		return value, err
	})
}

// Delete removes a key-value pair from a bucket. For seq-type buckets,
// key must be a string representation of the uint32 ID.
func Delete(dbPath, bucket, key string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return err
		}
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			var k []byte
			if kt == string(KeyTypeSeq) {
				var id uint64
				id, err = parseUint64(key)
				if err != nil {
					return errors.New("key must be an unsigned integer for seq-type bucket")
				}
				k = make([]byte, 4)
				binary.BigEndian.PutUint32(k, uint32(id))
			} else {
				k = []byte(key)
			}
			if b.Get(k) == nil {
				return ErrKeyNotFound
			}
			return b.Delete(k)
		})
	})
}

// PrefixScan returns all key-value pairs whose key starts with the given prefix.
func PrefixScan(dbPath, bucket, prefix string) (map[string]string, error) {
	return withDB1(dbPath, func(db *bolt.DB) (map[string]string, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string)
		err = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			c := b.Cursor()
			var seek []byte
			if kt == string(KeyTypeSeq) {
				var p uint64
				p, err = parseUint64(prefix)
				if err != nil {
					return errors.New("prefix must be an unsigned integer for seq-type bucket")
				}
				seek = make([]byte, 4)
				binary.BigEndian.PutUint32(seek, uint32(p))
			} else {
				seek = []byte(prefix)
			}
			for k, v := c.Seek(seek); k != nil && bytes.HasPrefix(k, seek); k, v = c.Next() {
				if kt == string(KeyTypeSeq) {
					out[uint32ToPadded10BE(k)] = string(v)
				} else {
					out[string(k)] = string(v)
				}
			}
			return nil
		})
		return out, err
	})
}

// RangeScan returns all key-value pairs whose key is in [start, end].
func RangeScan(dbPath, bucket, start, end string) (map[string]string, error) {
	return withDB1(dbPath, func(db *bolt.DB) (map[string]string, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string)
		err = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			c := b.Cursor()
			var s, e []byte
			if kt == string(KeyTypeSeq) {
				var su, eu uint64
				su, err = parseUint64(start)
				if err != nil {
					return errors.New("start must be an unsigned integer for seq-type bucket")
				}
				eu, err = parseUint64(end)
				if err != nil {
					return errors.New("end must be an unsigned integer for seq-type bucket")
				}
				s = make([]byte, 4)
				binary.BigEndian.PutUint32(s, uint32(su))
				e = make([]byte, 4)
				binary.BigEndian.PutUint32(e, uint32(eu))
			} else {
				s, e = []byte(start), []byte(end)
			}
			for k, v := c.Seek(s); k != nil && bytes.Compare(k, e) <= 0; k, v = c.Next() {
				if kt == string(KeyTypeSeq) {
					out[uint32ToPadded10BE(k)] = string(v)
				} else {
					out[string(k)] = string(v)
				}
			}
			return nil
		})
		return out, err
	})
}

// ScanAll returns all key-value pairs in a bucket.
func ScanAll(dbPath, bucket string) (map[string]string, error) {
	return withDB1(dbPath, func(db *bolt.DB) (map[string]string, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string)
		err = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			return b.ForEach(func(k, v []byte) error {
				if kt == string(KeyTypeSeq) {
					out[uint32ToPadded10BE(k)] = string(v)
				} else {
					out[string(k)] = string(v)
				}
				return nil
			})
		})
		return out, err
	})
}

// PartScan returns a portion of key-value pairs starting at the given offset
// and returning at most step entries.
func PartScan(dbPath, bucket string, start, step int) (map[string]string, error) {
	if start < 0 || step <= 0 {
		return nil, errors.New("start must be >= 0 and step must be > 0")
	}
	return withDB1(dbPath, func(db *bolt.DB) (map[string]string, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string)
		err = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			c := b.Cursor()
			k, v := c.First()
			for i := 0; i < start && k != nil; i++ {
				k, v = c.Next()
			}
			for i := 0; i < step && k != nil; i++ {
				if kt == string(KeyTypeSeq) {
					out[uint32ToPadded10BE(k)] = string(v)
				} else {
					out[string(k)] = string(v)
				}
				k, v = c.Next()
			}
			return nil
		})
		return out, err
	})
}

// Count returns the number of key-value pairs in a bucket.
func Count(dbPath, bucket string) (int, error) {
	return withDB1(dbPath, func(db *bolt.DB) (int, error) {
		var count int
		err := db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			count = b.Stats().KeyN
			return nil
		})
		return count, err
	})
}

// Export exports the entire database to a JSON file.
// For seq-type buckets, binary 4-byte keys are converted to padded10 strings
// so they survive JSON encoding without data loss.
func Export(dbPath, outPath string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		// Identify seq buckets from metadata
		seqBuckets := make(map[string]bool)
		if err := db.View(func(tx *bolt.Tx) error {
			mb := tx.Bucket([]byte(metadataBucket))
			if mb == nil {
				return nil
			}
			return mb.ForEach(func(k, v []byte) error {
				if string(v) == string(KeyTypeSeq) {
					seqBuckets[string(k)] = true
				}
				return nil
			})
		}); err != nil {
			return err
		}

		all := make(map[string]map[string]string)
		if err := db.View(func(tx *bolt.Tx) error {
			return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
				m := make(map[string]string)
				isSeq := seqBuckets[string(name)]
				err := b.ForEach(func(k, v []byte) error {
					if isSeq {
						m[uint32ToPadded10BE(k)] = string(v)
					} else {
						m[string(k)] = string(v)
					}
					return nil
				})
				all[string(name)] = m
				return err
			})
		}); err != nil {
			return err
		}
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	})
}

// Import imports data from a JSON file incrementally (creates buckets and
// key-value pairs as needed, without overwriting existing data).
// For seq-type buckets, padded10 string keys are converted back to binary
// 4-byte big-endian format.
func Import(dbPath, filePath string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		var all map[string]map[string]string
		if err := json.Unmarshal(data, &all); err != nil {
			return err
		}

		// Identify seq buckets from the imported metadata
		seqBuckets := make(map[string]bool)
		if meta, ok := all[metadataBucket]; ok {
			for name, kt := range meta {
				if kt == string(KeyTypeSeq) {
					seqBuckets[name] = true
				}
			}
		}

		return db.Update(func(tx *bolt.Tx) error {
			for bucketName, bucketData := range all {
				b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
				if err != nil {
					return err
				}
				isSeq := seqBuckets[bucketName]
				for k, v := range bucketData {
					if isSeq {
						id, err := parseUint64(k)
						if err != nil {
							return err
						}
						key := make([]byte, 4)
						binary.BigEndian.PutUint32(key, uint32(id))
						if err := b.Put(key, []byte(v)); err != nil {
							return err
						}
					} else {
						if err := b.Put([]byte(k), []byte(v)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		})
	})
}

// ImportReplace imports data from a JSON file, replacing all existing data.
// All existing buckets (except metadata) are deleted before import.
// For seq-type buckets, padded10 string keys are converted back to binary
// 4-byte big-endian format.
func ImportReplace(dbPath, filePath string) error {
	return withDB(dbPath, func(db *bolt.DB) error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		var all map[string]map[string]string
		if err := json.Unmarshal(data, &all); err != nil {
			return err
		}

		// Identify seq buckets from the imported metadata
		seqBuckets := make(map[string]bool)
		if meta, ok := all[metadataBucket]; ok {
			for name, kt := range meta {
				if kt == string(KeyTypeSeq) {
					seqBuckets[name] = true
				}
			}
		}

		var bucketNames []string
		if err := db.View(func(tx *bolt.Tx) error {
			return tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
				bucketNames = append(bucketNames, string(name))
				return nil
			})
		}); err != nil {
			return err
		}
		return db.Update(func(tx *bolt.Tx) error {
			for _, name := range bucketNames {
				if err := tx.DeleteBucket([]byte(name)); err != nil {
					return err
				}
			}
			for bucketName, bucketData := range all {
				b, err := tx.CreateBucket([]byte(bucketName))
				if err != nil {
					return err
				}
				isSeq := seqBuckets[bucketName]
				for k, v := range bucketData {
					if isSeq {
						id, err := parseUint64(k)
						if err != nil {
							return err
						}
						key := make([]byte, 4)
						binary.BigEndian.PutUint32(key, uint32(id))
						if err := b.Put(key, []byte(v)); err != nil {
							return err
						}
					} else {
						if err := b.Put([]byte(k), []byte(v)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		})
	})
}

// Info returns statistics for a given bucket as a map of metric names to values.
func Info(dbPath, bucket string) (map[string]int, error) {
	return withDB1(dbPath, func(db *bolt.DB) (map[string]int, error) {
		info := make(map[string]int)
		err := db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			stats := b.Stats()
			info["BranchAlloc"] = int(stats.BranchAlloc)
			info["BranchInuse"] = int(stats.BranchInuse)
			info["BranchOverflowN"] = int(stats.BranchOverflowN)
			info["BranchPageN"] = int(stats.BranchPageN)
			info["BucketN"] = int(stats.BucketN)
			info["Depth"] = int(stats.Depth)
			info["InlineBucketInuse"] = int(stats.InlineBucketInuse)
			info["InlineBucketN"] = int(stats.InlineBucketN)
			info["KeyN"] = int(stats.KeyN)
			info["LeafAlloc"] = int(stats.LeafAlloc)
			info["LeafInuse"] = int(stats.LeafInuse)
			info["LeafOverflowN"] = int(stats.LeafOverflowN)
			info["LeafPageN"] = int(stats.LeafPageN)
			return nil
		})
		return info, err
	})
}

// BucketKeyType returns the stored key type for a bucket.
func BucketKeyType(dbPath, bucket string) (string, error) {
	return withDB1(dbPath, func(db *bolt.DB) (string, error) {
		return getKeyType(db, bucket)
	})
}

// BucketExists checks whether a bucket exists.
func BucketExists(dbPath, bucket string) (bool, error) {
	return withDB1(dbPath, func(db *bolt.DB) (bool, error) {
		err := db.View(func(tx *bolt.Tx) error {
			if tx.Bucket([]byte(bucket)) == nil {
				return ErrBucketNotFound
			}
			return nil
		})
		if err == ErrBucketNotFound {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

// KeyExists checks whether a key exists in a bucket.
func KeyExists(dbPath, bucket, key string) (bool, error) {
	return withDB1(dbPath, func(db *bolt.DB) (bool, error) {
		kt, err := getKeyType(db, bucket)
		if err != nil {
			return false, err
		}
		err = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return ErrBucketNotFound
			}
			var k []byte
			if kt == string(KeyTypeSeq) {
				var id uint64
				id, err = parseUint64(key)
				if err != nil {
					return errors.New("key must be an unsigned integer for seq-type bucket")
				}
				k = make([]byte, 4)
				binary.BigEndian.PutUint32(k, uint32(id))
			} else {
				k = []byte(key)
			}
			if b.Get(k) == nil {
				return ErrKeyNotFound
			}
			return nil
		})
		if err == ErrKeyNotFound {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

// parseUint64 parses a decimal string into a uint64.
func parseUint64(s string) (uint64, error) {
	if len(s) == 0 {
		return 0, errors.New("empty string")
	}
	var n uint64
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid digit")
		}
		n = n*10 + uint64(c-'0')
	}
	return n, nil
}
