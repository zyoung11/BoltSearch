package engine

type Document struct {
	ID      uint64 `msgpack:"id"`
	Title   string `msgpack:"title"`
	Content string `msgpack:"content"`
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

type Stats struct {
	TotalDocs    uint64
	TotalTokens  uint64
	AvgDocLen    float64
	UniqueTerms  int
	DBFileSize   int64
}
