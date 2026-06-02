package engine

import "github.com/kljensen/snowball/english"

func stem(word string) string {
	if len(word) <= 2 {
		return word
	}
	return english.Stem(word, false)
}
