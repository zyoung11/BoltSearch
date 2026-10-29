package engine

type Document struct {
	ID      uint64            `msgpack:"id"`
	Title   string            `msgpack:"title"`
	Content string            `msgpack:"content"`
	Meta    map[string]string `msgpack:"meta"`
}

type Token struct {
	Term     string
	Position int
}

type Posting struct {
	DocID     uint64 `msgpack:"did"`
	TF        int    `msgpack:"tf"`
	Positions []int  `msgpack:"ps"`
}

type PostingList struct {
	Postings []Posting `msgpack:"pl"`
}

type IndexMeta struct {
	NextDocID   uint64 `msgpack:"nd"`
	TotalDocs   uint64 `msgpack:"td"`
	TotalTokens uint64 `msgpack:"tt"`
}

type ScoredDoc struct {
	Doc        Document
	Score      float64
	MatchedOn  []string
	MatchCount int
}

// SearchOptions 保存一次搜索的全部选项。
type SearchOptions struct {
	Mode       string
	Limit      int
	Offset     int
	Prefix     bool
	Fuzzy      bool
	Phrase     bool
	MetaFilter map[string]string
}

type Stats struct {
	TotalDocs   uint64
	TotalTokens uint64
	AvgDocLen   float64
	UniqueTerms int
	DBFileSize  int64
}
