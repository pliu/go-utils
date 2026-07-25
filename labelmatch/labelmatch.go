// Package labelmatch matches label sets against a set of rules and applies
// the rules' writes back onto the label set.
//
// A Rule pairs Alertmanager-style matchers (=, !=, =~, !~, all ANDed) with a
// set of labels to write. Compile turns a slice of Rules into an immutable,
// concurrency-safe RuleSet; RuleSet.Apply evaluates every rule against a
// label set and then mutates it in a single final pass.
//
// # Rule order does not matter
//
// Matching and mutation are strictly separated. Every matcher is evaluated
// against the original, unmodified label set, and no write is visible to any
// matcher — so a rule can never observe another rule's output, and permuting
// the slice passed to Compile cannot change the result.
//
// Writes from different rules to the same label are aggregated rather than
// treated as a conflict. Values are tokenized on whitespace, merged with the
// tokens already present in the label, deduplicated, sorted, and rejoined
// with single spaces:
//
//	labels: {"team": "x"}
//	rule A: write {"team": "a"}
//	rule B: write {"team": "b x"}
//	result: {"team": "a b x"}
//
// Sorting is what makes aggregation order-independent, and merging with the
// existing value makes Apply idempotent: applying the same RuleSet twice to
// the same label set produces the same result, and the second call reports
// no change.
package labelmatch

import (
	"fmt"
	"slices"
	"sync"
)

// Op is a matcher operator. It mirrors Alertmanager's matcher syntax.
type Op string

// Supported matcher operators. Regex operators are anchored on both ends:
// OpRegex matches only if the pattern matches the whole label value.
const (
	OpEqual    Op = "="  // label value equals Value
	OpNotEqual Op = "!=" // label value differs from Value
	OpRegex    Op = "=~" // label value fully matches the regex in Value
	OpNotRegex Op = "!~" // label value does not fully match the regex in Value
)

// Matcher is a single condition on one label.
//
// A label that is absent from the label set is treated as the empty string,
// as in Alertmanager. Consequently {Name: "env", Op: OpEqual, Value: ""}
// matches label sets with no "env" label, and {Name: "env", Op: OpNotEqual,
// Value: "prod"} matches them too.
type Matcher struct {
	Name  string `json:"name"`
	Op    Op     `json:"op"`
	Value string `json:"value"`
}

// Rule is a set of matchers and the labels to write when they all match.
//
// A Rule with no matchers matches every label set, which is useful for
// unconditional enrichment. Write must be non-empty: a rule that cannot
// change anything is rejected by Compile.
type Rule struct {
	// Name is optional and used only to identify the rule in Compile errors.
	Name string `json:"name,omitempty"`

	// Matchers must all match for the rule to apply.
	Matchers []Matcher `json:"matchers"`

	// Write maps label names to the values this rule contributes. Values are
	// aggregated with the label's existing value and with the contributions
	// of every other matching rule; see the package documentation.
	Write map[string]string `json:"write"`
}

// RuleError identifies which rule failed to compile.
type RuleError struct {
	Index int    // position in the slice passed to Compile
	Name  string // Rule.Name, if set
	Err   error
}

func (e *RuleError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("labelmatch: rule %d (%s): %v", e.Index, e.Name, e.Err)
	}
	return fmt.Sprintf("labelmatch: rule %d: %v", e.Index, e.Err)
}

func (e *RuleError) Unwrap() error { return e.Err }

// pair is an (label name, label value) index key.
type pair struct {
	name  string
	value string
}

// RuleSet is a compiled, immutable set of rules. It is safe for concurrent
// use by multiple goroutines.
type RuleSet struct {
	rules []compiledRule

	// index maps an equality matcher that a rule requires to the rules
	// selected by it, so Apply only evaluates rules that can possibly match.
	// Each rule appears in exactly one bucket, so matches never need
	// deduplication.
	index map[pair][]uint32

	// names holds the distinct label names appearing in index, letting Apply
	// drive the lookup from whichever of the two sides is smaller.
	names []string

	// always holds rules with no indexable matcher; they are evaluated on
	// every call.
	always []uint32
}

// Len reports the number of compiled rules.
func (rs *RuleSet) Len() int {
	if rs == nil {
		return 0
	}
	return len(rs.rules)
}

// Apply evaluates every rule against labels and then mutates labels in a
// single pass, returning whether anything changed.
//
// All matching happens before any mutation, so rules always see the original
// label set and the order of rules is irrelevant. Writes to the same label
// from multiple rules are aggregated as described in the package docs.
//
// Apply does not copy labels and does not synchronize access to it: the
// caller must not read or write the map concurrently. Applying different
// RuleSets, or the same RuleSet to different maps, from multiple goroutines
// is safe. A nil map is left alone and reports no change.
func (rs *RuleSet) Apply(labels map[string]string) bool {
	if rs == nil || len(rs.rules) == 0 || labels == nil {
		return false
	}

	s := getScratch()
	defer putScratch(s)

	s.matched = rs.collectMatches(s.matched[:0], labels)
	if len(s.matched) == 0 {
		return false
	}

	// Gather every contribution per label before touching the map.
	s.accums = s.accums[:0]
	for _, ri := range s.matched {
		writes := rs.rules[ri].writes
		for i := range writes {
			w := &writes[i]
			a := s.accumFor(w.key)
			a.contribs++
			if a.contribs == 1 {
				a.canon = w.canon
			}
			a.tokens = append(a.tokens, w.tokens...)
		}
	}

	changed := false
	for i := range s.accums {
		a := &s.accums[i]
		existing, present := labels[a.key]

		// Fast path: a single contributor whose canonical form is already
		// correct, or a label that does not exist yet. Neither allocates.
		if a.contribs == 1 {
			if !present || existing == "" {
				labels[a.key] = a.canon
				changed = true
				continue
			}
			if existing == a.canon {
				continue
			}
		}

		a.tokens = appendFields(a.tokens, existing)
		slices.Sort(a.tokens)
		a.tokens = slices.Compact(a.tokens)

		s.buf = joinInto(s.buf[:0], a.tokens)
		// The compiler elides the allocation for this comparison; the
		// conversion below only happens when the value really changes.
		if present && string(s.buf) == existing {
			continue
		}
		labels[a.key] = string(s.buf)
		changed = true
	}

	return changed
}

// Matched returns the indices, into the slice originally passed to Compile,
// of the rules matching labels. It is intended for diagnostics and testing;
// Apply does not use it and does not allocate a result slice. The indices
// are not sorted.
func (rs *RuleSet) Matched(labels map[string]string) []int {
	if rs == nil || len(rs.rules) == 0 {
		return nil
	}
	s := getScratch()
	defer putScratch(s)

	s.matched = rs.collectMatches(s.matched[:0], labels)
	if len(s.matched) == 0 {
		return nil
	}
	out := make([]int, len(s.matched))
	for i, ri := range s.matched {
		out[i] = rs.rules[ri].index
	}
	return out
}

// collectMatches appends the compiled index of every matching rule to dst.
func (rs *RuleSet) collectMatches(dst []uint32, labels map[string]string) []uint32 {
	for _, ri := range rs.always {
		if rs.rules[ri].match(labels) {
			dst = append(dst, ri)
		}
	}
	if len(rs.index) == 0 {
		return dst
	}

	// Probe from whichever side has fewer entries: walking the indexed names
	// wins for small rule sets, walking the label set wins for large ones.
	if len(rs.names) < len(labels) {
		for _, name := range rs.names {
			value, ok := labels[name]
			if !ok {
				continue
			}
			dst = rs.probe(dst, pair{name, value}, labels)
		}
		return dst
	}
	for name, value := range labels {
		dst = rs.probe(dst, pair{name, value}, labels)
	}
	return dst
}

func (rs *RuleSet) probe(dst []uint32, p pair, labels map[string]string) []uint32 {
	for _, ri := range rs.index[p] {
		if rs.rules[ri].match(labels) {
			dst = append(dst, ri)
		}
	}
	return dst
}

// compiledRule is the internal form of a Rule.
type compiledRule struct {
	// matchers are ordered cheapest-first and exclude the matcher this rule
	// is indexed by, which is known to hold whenever the rule is reached.
	matchers []matcher
	writes   []write
	index    int // position in the slice passed to Compile
}

func (r *compiledRule) match(labels map[string]string) bool {
	for i := range r.matchers {
		if !r.matchers[i].match(labels) {
			return false
		}
	}
	return true
}

// write is a precanonicalized contribution to one label.
type write struct {
	key    string
	canon  string   // tokens joined with single spaces
	tokens []string // sorted, deduplicated
}

// matcher is the compiled form of a Matcher.
type matcher struct {
	name  string
	value string
	re    *fastRegex // nil unless op is OpRegex or OpNotRegex
	op    Op
}

func (m *matcher) match(labels map[string]string) bool {
	// A missing label is the empty string, as in Alertmanager.
	v := labels[m.name]
	switch m.op {
	case OpEqual:
		return v == m.value
	case OpNotEqual:
		return v != m.value
	case OpRegex:
		return m.re.matchString(v)
	default: // OpNotRegex
		return !m.re.matchString(v)
	}
}

// cost orders matchers within a rule so the cheapest tests run first and
// short-circuit the expensive ones.
func (m *matcher) cost() int {
	switch m.op {
	case OpEqual, OpNotEqual:
		return 0
	default:
		return m.re.cost()
	}
}

// scratch holds the per-Apply working state. Instances are pooled so a warm
// process performs no allocation while matching or aggregating.
type scratch struct {
	matched []uint32
	accums  []accum
	buf     []byte
}

// accum aggregates every contribution to a single label.
type accum struct {
	key      string
	canon    string
	tokens   []string
	contribs int
}

// accumFor returns the accumulator for key, creating it if needed. The
// number of distinct written labels is small, so a linear scan beats a map
// and avoids allocating one per call.
func (s *scratch) accumFor(key string) *accum {
	for i := range s.accums {
		if s.accums[i].key == key {
			return &s.accums[i]
		}
	}
	if n := len(s.accums); n < cap(s.accums) {
		// Reuse the token backing array left by a previous Apply.
		s.accums = s.accums[:n+1]
		a := &s.accums[n]
		a.key = key
		a.canon = ""
		a.tokens = a.tokens[:0]
		a.contribs = 0
		return a
	}
	s.accums = append(s.accums, accum{key: key})
	return &s.accums[len(s.accums)-1]
}

var scratchPool = sync.Pool{New: func() any { return new(scratch) }}

func getScratch() *scratch { return scratchPool.Get().(*scratch) }

// Limits on what is worth keeping pooled; an unusually large label set or
// rule set should not permanently inflate every pooled scratch.
const (
	maxPooledMatches = 1024
	maxPooledAccums  = 128
	maxPooledBuf     = 8192
)

func putScratch(s *scratch) {
	if cap(s.matched) > maxPooledMatches || cap(s.accums) > maxPooledAccums || cap(s.buf) > maxPooledBuf {
		return
	}
	// Drop references to the caller's label strings so the pool does not
	// keep them alive until the next Apply. Clearing the full capacity, not
	// just the length, also catches entries left behind by a longer earlier
	// call whose slice was later truncated for reuse.
	full := s.accums[:cap(s.accums)]
	for i := range full {
		a := &full[i]
		a.key = ""
		a.canon = ""
		clear(a.tokens[:cap(a.tokens)])
	}
	scratchPool.Put(s)
}
