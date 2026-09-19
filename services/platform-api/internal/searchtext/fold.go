// Package searchtext adds tolerant character recall without changing stored names.
package searchtext

import (
	_ "embed"
	"encoding/json"
	"strings"

	"golang.org/x/text/unicode/norm"
)

//go:embed data/variants.json
var data []byte

var Source, Target string
var variants map[rune]rune

func init() {
	var table struct{ Source, Target string }
	if err := json.Unmarshal(data, &table); err != nil {
		panic("invalid embedded search variants")
	}
	from, to := []rune(table.Source), []rune(table.Target)
	if len(from) == 0 || len(from) != len(to) {
		panic("mismatched embedded search variants")
	}
	Source, Target = table.Source, table.Target
	variants = make(map[rune]rune, len(from))
	for i, c := range from {
		variants[c] = to[i]
	}
}

func Fold(value string) string {
	value = strings.ToLower(norm.NFKC.String(value))
	return strings.Map(func(c rune) rune {
		if v, ok := variants[c]; ok {
			return v
		}
		return c
	}, value)
}

// TableFor includes only equivalence classes present in the folded query.
// Folding unrelated characters cannot affect a literal substring match; keeping
// the SQL translate table small avoids scanning 4,000 variants per stored rune.
func TableFor(query string) (string, string) {
	wanted := map[rune]bool{}
	for _, c := range Fold(query) {
		wanted[c] = true
	}
	var from, to strings.Builder
	for _, c := range Source {
		v := variants[c]
		if wanted[v] {
			from.WriteRune(c)
			to.WriteRune(v)
		}
	}
	return from.String(), to.String()
}
