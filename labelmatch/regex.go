package labelmatch

import (
	"regexp"
	"regexp/syntax"
)

// maxLiteralSet bounds how many alternatives are expanded into a set. Beyond
// this, the regexp engine is cheaper than building and probing a large map,
// and the expansion itself could blow up on nested alternations.
const maxLiteralSet = 128

type regexKind uint8

const (
	kindLiteral regexKind = iota // exactly one possible value
	kindSet                      // a small, closed set of possible values
	kindRegex                    // needs the regexp engine
)

// fastRegex is an anchored regex that degrades to a string or map comparison
// when the pattern permits. Anchoring is what makes this sound: because the
// pattern must match the entire label value, a pattern whose language is a
// finite set of literals is equivalent to membership in that set.
type fastRegex struct {
	lit  string
	set  map[string]struct{}
	re   *regexp.Regexp
	kind regexKind
}

// compileRegex compiles expr as a fully anchored pattern.
func compileRegex(expr string) (*fastRegex, error) {
	parsed, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil, err
	}

	if lits, ok := literalAlternatives(parsed.Simplify()); ok && len(lits) > 0 {
		if len(lits) == 1 {
			return &fastRegex{kind: kindLiteral, lit: lits[0]}, nil
		}
		set := make(map[string]struct{}, len(lits))
		for _, l := range lits {
			set[l] = struct{}{}
		}
		if len(set) == 1 {
			return &fastRegex{kind: kindLiteral, lit: lits[0]}, nil
		}
		return &fastRegex{kind: kindSet, set: set}, nil
	}

	// (?s:...) so that . spans newlines, matching Alertmanager, and \A/\z
	// rather than ^/$ so that an embedded (?m) cannot unanchor the pattern.
	re, err := regexp.Compile(`\A(?s:` + expr + `)\z`)
	if err != nil {
		return nil, err
	}
	return &fastRegex{kind: kindRegex, re: re}, nil
}

func (f *fastRegex) matchString(s string) bool {
	switch f.kind {
	case kindLiteral:
		return s == f.lit
	case kindSet:
		_, ok := f.set[s]
		return ok
	default:
		return f.re.MatchString(s)
	}
}

// literal reports the single value this regex accepts, if there is exactly
// one.
func (f *fastRegex) literal() (string, bool) {
	if f.kind == kindLiteral {
		return f.lit, true
	}
	return "", false
}

func (f *fastRegex) cost() int {
	if f.kind == kindSet {
		return 1
	}
	return 2
}

// literalAlternatives returns the complete, finite set of strings re matches,
// or false if that set is unbounded or too large to be worth enumerating.
func literalAlternatives(re *syntax.Regexp) ([]string, bool) {
	switch re.Op {
	case syntax.OpEmptyMatch:
		return []string{""}, true

	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return nil, false
		}
		return []string{string(re.Rune)}, true

	case syntax.OpCapture:
		return literalAlternatives(re.Sub[0])

	case syntax.OpCharClass:
		if re.Flags&syntax.FoldCase != 0 {
			return nil, false
		}
		var out []string
		for i := 0; i+1 < len(re.Rune); i += 2 {
			for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
				if len(out) >= maxLiteralSet {
					return nil, false
				}
				out = append(out, string(r))
			}
		}
		return out, len(out) > 0

	case syntax.OpConcat:
		out := []string{""}
		for _, sub := range re.Sub {
			subs, ok := literalAlternatives(sub)
			if !ok || len(out)*len(subs) > maxLiteralSet {
				return nil, false
			}
			next := make([]string, 0, len(out)*len(subs))
			for _, prefix := range out {
				for _, suffix := range subs {
					next = append(next, prefix+suffix)
				}
			}
			out = next
		}
		return out, true

	case syntax.OpAlternate:
		var out []string
		for _, sub := range re.Sub {
			subs, ok := literalAlternatives(sub)
			if !ok || len(out)+len(subs) > maxLiteralSet {
				return nil, false
			}
			out = append(out, subs...)
		}
		return out, true
	}

	// Anything else (repetition, any-char, word boundaries, anchors) is
	// either unbounded or not worth enumerating.
	return nil, false
}
