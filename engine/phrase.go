package engine

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	phraseMinRunes      = 3
	phraseCoverageFloor = 0.5
	phraseExactBonus    = 1000.0
	phraseCoverageBonus = 300.0
)

type phraseEntry struct {
	doc   Document
	plain string
}

// phraseIndex 保存全部文档的归一化原文，为查询提供短语通道加分。
type phraseIndex struct {
	entries []phraseEntry
}

func newPhraseIndex(docs []Document) *phraseIndex {
	index := &phraseIndex{entries: make([]phraseEntry, 0, len(docs))}
	for _, doc := range docs {
		entry := phraseEntry{doc: doc}
		entry.plain = normalizeText(doc.Title + "\n" + doc.Content)
		index.entries = append(index.entries, entry)
	}
	return index
}

// normalizeText 去掉空白与标点并折叠大小写，只保留文字与数字，使短语匹配对粘贴差异不敏感。
func normalizeText(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func queryGramSet(normalized string) (map[string]bool, int) {
	runes := []rune(normalized)
	set := make(map[string]bool)
	for i := 0; i+1 < len(runes); i++ {
		set[string(runes[i:i+2])] = true
	}
	return set, len(runes) - 1
}

func countMatchedGrams(text string, set map[string]bool) int {
	matched := make(map[string]bool, len(set))
	prev := -1
	for start, r := range text {
		if prev >= 0 {
			gram := text[prev : start+utf8.RuneLen(r)]
			if set[gram] {
				matched[gram] = true
			}
		}
		prev = start
	}
	return len(matched)
}

// bonuses 对查询做字面子串与二元切片覆盖评估，返回各文档的短语加分。
func (p *phraseIndex) bonuses(query string) map[uint64]float64 {
	normalized := normalizeText(query)
	if utf8.RuneCountInString(normalized) < phraseMinRunes {
		return nil
	}
	set, total := queryGramSet(normalized)
	if total <= 0 {
		return nil
	}
	bonus := make(map[uint64]float64)
	for _, entry := range p.entries {
		coverage := float64(countMatchedGrams(entry.plain, set)) / float64(total)
		if coverage < phraseCoverageFloor && !strings.Contains(entry.plain, normalized) {
			continue
		}
		score := 0.0
		if strings.Contains(entry.plain, normalized) {
			score += phraseExactBonus
		}
		if coverage >= phraseCoverageFloor {
			score += phraseCoverageBonus * coverage * coverage
		}
		bonus[entry.doc.ID] = score
	}
	return bonus
}
