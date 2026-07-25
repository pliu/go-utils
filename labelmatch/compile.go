package labelmatch

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
)

// Compile validates rules and builds an immutable RuleSet.
//
// It returns a *RuleError identifying the offending rule if a matcher has an
// empty name or an unknown operator, a regex fails to compile, Write is
// empty, or a written label name or value is empty. Compiling an empty slice
// yields a RuleSet whose Apply is a no-op.
//
// Compile does all the work that does not depend on the label set: regexes
// are anchored and reduced to string or set comparisons where possible,
// written values are canonicalized, matchers are ordered cheapest-first, and
// rules are indexed by their most selective equality matcher.
func Compile(rules []Rule) (*RuleSet, error) {
	return compile(rules, true)
}

// compile builds a RuleSet, optionally without the selectivity index. The
// unindexed form evaluates every rule on every call; it is equivalent but
// slower, and exists so tests and benchmarks can compare against it.
func compile(rules []Rule, withIndex bool) (*RuleSet, error) {
	if len(rules) > math.MaxUint32 {
		return nil, errors.New("labelmatch: too many rules")
	}

	rs := &RuleSet{rules: make([]compiledRule, 0, len(rules))}

	// candidates[i] holds the equality matchers of rule i that can serve as
	// its index key, along with their position in the rule's matcher slice.
	type candidate struct {
		p  pair
		mi int
	}
	candidates := make([][]candidate, len(rules))
	freq := make(map[pair]int)

	for i := range rules {
		cr, cands, err := compileRule(&rules[i], i)
		if err != nil {
			return nil, &RuleError{Index: i, Name: rules[i].Name, Err: err}
		}
		for _, c := range cands {
			candidates[i] = append(candidates[i], candidate{p: c.p, mi: c.mi})
			freq[c.p]++
		}
		rs.rules = append(rs.rules, cr)
	}

	// Index each rule by its rarest equality matcher. Rarest is the best
	// available proxy for most selective: it keeps buckets small, so a
	// lookup that hits still evaluates few rules.
	for i := range rs.rules {
		best := -1
		bestFreq := math.MaxInt
		if withIndex {
			for ci, c := range candidates[i] {
				if f := freq[c.p]; f < bestFreq {
					best, bestFreq = ci, f
				}
			}
		}
		if best < 0 {
			rs.always = append(rs.always, uint32(i))
			continue
		}
		c := candidates[i][best]
		if rs.index == nil {
			rs.index = make(map[pair][]uint32)
		}
		rs.index[c.p] = append(rs.index[c.p], uint32(i))
		// The index key is guaranteed true whenever the rule is reached, so
		// re-testing it would be wasted work.
		r := &rs.rules[i]
		r.matchers = slices.Delete(r.matchers, c.mi, c.mi+1)
	}

	for i := range rs.rules {
		m := rs.rules[i].matchers
		sort.SliceStable(m, func(a, b int) bool { return m[a].cost() < m[b].cost() })
	}

	for p := range rs.index {
		if !slices.Contains(rs.names, p.name) {
			rs.names = append(rs.names, p.name)
		}
	}
	slices.Sort(rs.names)

	return rs, nil
}

type indexCandidate struct {
	p  pair
	mi int
}

func compileRule(r *Rule, index int) (compiledRule, []indexCandidate, error) {
	cr := compiledRule{index: index}

	if len(r.Write) == 0 {
		return cr, nil, errors.New("write must contain at least one label")
	}

	keys := make([]string, 0, len(r.Write))
	for k := range r.Write {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	cr.writes = make([]write, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			return cr, nil, errors.New("write contains an empty label name")
		}
		canon, tokens := canonicalize(r.Write[k])
		if len(tokens) == 0 {
			return cr, nil, fmt.Errorf("write for label %q has no value", k)
		}
		cr.writes = append(cr.writes, write{key: k, canon: canon, tokens: tokens})
	}

	var cands []indexCandidate
	cr.matchers = make([]matcher, 0, len(r.Matchers))
	for i := range r.Matchers {
		m, err := compileMatcher(&r.Matchers[i])
		if err != nil {
			return cr, nil, err
		}
		// Only a non-empty equality match is indexable: Value "" also
		// matches label sets where the label is absent, so there is no
		// (name, value) pair in the input to look it up by.
		if m.op == OpEqual && m.value != "" {
			cands = append(cands, indexCandidate{p: pair{m.name, m.value}, mi: len(cr.matchers)})
		}
		cr.matchers = append(cr.matchers, m)
	}

	return cr, cands, nil
}

func compileMatcher(m *Matcher) (matcher, error) {
	if m.Name == "" {
		return matcher{}, errors.New("matcher has an empty label name")
	}
	switch m.Op {
	case OpEqual, OpNotEqual:
		return matcher{name: m.Name, value: m.Value, op: m.Op}, nil
	case OpRegex, OpNotRegex:
		re, err := compileRegex(m.Value)
		if err != nil {
			return matcher{}, fmt.Errorf("matcher on label %q: %w", m.Name, err)
		}
		// An anchored regex that reduces to a single literal is exactly an
		// equality test. Rewriting it makes the matcher branch-free and, for
		// =~, makes the rule eligible for indexing.
		if lit, ok := re.literal(); ok {
			op := OpEqual
			if m.Op == OpNotRegex {
				op = OpNotEqual
			}
			return matcher{name: m.Name, value: lit, op: op}, nil
		}
		return matcher{name: m.Name, value: m.Value, op: m.Op, re: re}, nil
	default:
		return matcher{}, fmt.Errorf("unknown operator %q", m.Op)
	}
}
