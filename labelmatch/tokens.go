package labelmatch

import "slices"

// Label values are treated as whitespace-separated token sets so that
// contributions from several rules can be merged. Canonical form is the
// tokens, deduplicated and sorted, joined by single spaces.

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// appendFields appends the whitespace-separated tokens of s to dst. The
// tokens share s's backing array, so splitting itself never allocates, and
// reusing dst across calls avoids allocating the slice too.
//
// Splitting on ASCII whitespace bytes is UTF-8 safe: every byte of a
// multi-byte rune has its high bit set and so can never be mistaken for one.
func appendFields(dst []string, s string) []string {
	start := -1
	for i := 0; i < len(s); i++ {
		if isSpace(s[i]) {
			if start >= 0 {
				dst = append(dst, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		dst = append(dst, s[start:])
	}
	return dst
}

// joinInto appends tokens to dst separated by single spaces.
func joinInto(dst []byte, tokens []string) []byte {
	for i, t := range tokens {
		if i > 0 {
			dst = append(dst, ' ')
		}
		dst = append(dst, t...)
	}
	return dst
}

// canonicalize returns v's canonical form along with its sorted, deduplicated
// tokens. It runs at compile time only.
func canonicalize(v string) (string, []string) {
	tokens := appendFields(nil, v)
	if len(tokens) == 0 {
		return "", nil
	}
	slices.Sort(tokens)
	tokens = slices.Clip(slices.Compact(tokens))
	return string(joinInto(make([]byte, 0, len(v)), tokens)), tokens
}
