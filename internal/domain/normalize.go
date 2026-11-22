package domain

import (
	"strconv"
	"strings"
	"unicode"
)

var diacritics = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y", "ÿ", "y",
)

// Normalize lowercases, strips common Latin diacritics and collapses whitespace.
func Normalize(s string) string {
	s = diacritics.Replace(strings.ToLower(s))
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// DuplicateKey is the MVP duplicate candidate rule (BR-01, FR-W-07):
// normalized title + year + normalized first author.
func DuplicateKey(w Work) string {
	return Normalize(w.Title) + "|" + strconv.Itoa(w.Year) + "|" + Normalize(w.FirstAuthor())
}
