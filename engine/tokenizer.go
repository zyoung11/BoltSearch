package engine

import (
	"io"
	"log"
	"os"
	"strings"
	"unicode"

	"github.com/go-ego/gse"
)

var stopWords = map[string]bool{}

func init() {
	words := []string{
		"a", "an", "the", "and", "or", "but", "in", "on", "at", "to",
		"for", "of", "with", "is", "are", "was", "were", "be", "been",
		"being", "have", "has", "had", "do", "does", "did", "will",
		"would", "shall", "should", "may", "might", "must", "can",
		"could", "it", "its", "this", "that", "these", "those", "i",
		"me", "my", "we", "our", "you", "your", "he", "she", "him",
		"his", "her", "they", "them", "their", "what", "which", "who",
		"whom", "when", "where", "why", "how", "all", "each", "every",
		"both", "few", "more", "most", "other", "some", "such", "no",
		"not", "only", "same", "so", "than", "too", "very", "just",
		"about", "up", "out", "if", "then", "here", "there", "from",
		"as", "by", "into", "through", "during", "before", "after",
		"above", "below", "between", "under", "again", "further",
		"once", "also", "now", "well", "still", "down", "off", "over",
		"的", "了", "在", "是", "我", "有", "和", "就", "不", "人",
		"都", "一", "上", "也", "很", "到", "说", "要", "去", "你",
		"会", "着", "没有", "看", "好", "自己", "这", "他", "她",
		"它", "们", "那", "什么", "怎么", "一个", "这个", "那个",
		"可以", "因为", "所以", "但是", "虽然", "如果", "已经",
		"还", "被", "把", "让", "给", "从", "对", "向", "吗",
		"呢", "吧", "啊", "呀", "哦", "嗯", "么", "嘛",
	}
	for _, w := range words {
		stopWords[w] = true
	}
}

type Tokenizer struct {
	seg gse.Segmenter
}

func NewTokenizer() (*Tokenizer, error) {
	t := &Tokenizer{}
	log.SetOutput(io.Discard)
	seg, err := gse.NewEmbed()
	t.seg = seg
	log.SetOutput(os.Stderr)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func containsChinese(text string) bool {
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func isAllASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func isValidToken(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func (t *Tokenizer) Tokenize(text string) []Token {
	var rawTokens []string
	if containsChinese(text) {
		rawTokens = t.seg.CutSearch(text, true)
	} else {
		rawTokens = splitEnglish(text)
	}

	result := make([]Token, 0, len(rawTokens))
	pos := 0
	for _, term := range rawTokens {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if !isValidToken(term) {
			continue
		}
		term = strings.ToLower(term)
		if isAllASCII(term) {
			term = stem(term)
		}
		if stopWords[term] {
			continue
		}
		result = append(result, Token{Term: term, Position: pos})
		pos++
	}
	return result
}

func splitEnglish(text string) []string {
	var result []string
	var current []rune
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				result = append(result, string(current))
				current = current[:0]
			}
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}
