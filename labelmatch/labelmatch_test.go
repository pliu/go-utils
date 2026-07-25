package labelmatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustCompile(t *testing.T, rules []Rule) *RuleSet {
	t.Helper()
	rs, err := Compile(rules)
	require.NoError(t, err)
	return rs
}

func TestMatcherOperators(t *testing.T) {
	tests := []struct {
		name    string
		matcher Matcher
		labels  map[string]string
		want    bool
	}{
		{"equal hit", Matcher{"env", OpEqual, "prod"}, map[string]string{"env": "prod"}, true},
		{"equal miss", Matcher{"env", OpEqual, "prod"}, map[string]string{"env": "dev"}, false},
		{"not equal hit", Matcher{"env", OpNotEqual, "prod"}, map[string]string{"env": "dev"}, true},
		{"not equal miss", Matcher{"env", OpNotEqual, "prod"}, map[string]string{"env": "prod"}, false},
		{"regex hit", Matcher{"env", OpRegex, "pro.*"}, map[string]string{"env": "prod"}, true},
		{"regex miss", Matcher{"env", OpRegex, "pro.*"}, map[string]string{"env": "dev"}, false},
		{"not regex hit", Matcher{"env", OpNotRegex, "pro.*"}, map[string]string{"env": "dev"}, true},
		{"not regex miss", Matcher{"env", OpNotRegex, "pro.*"}, map[string]string{"env": "prod"}, false},

		// A missing label is the empty string, as in Alertmanager.
		{"missing equals empty", Matcher{"env", OpEqual, ""}, map[string]string{}, true},
		{"missing not equal", Matcher{"env", OpNotEqual, "prod"}, map[string]string{}, true},
		{"missing regex empty", Matcher{"env", OpRegex, ".*"}, map[string]string{}, true},
		{"missing regex nonempty", Matcher{"env", OpRegex, ".+"}, map[string]string{}, false},
		{"missing not regex", Matcher{"env", OpNotRegex, "prod"}, map[string]string{}, true},
		{"present empty equals empty", Matcher{"env", OpEqual, ""}, map[string]string{"env": ""}, true},

		// Regexes are anchored on both ends.
		{"regex is anchored at end", Matcher{"env", OpRegex, "prod"}, map[string]string{"env": "production"}, false},
		{"regex is anchored at start", Matcher{"env", OpRegex, "duction"}, map[string]string{"env": "production"}, false},
		{"regex anchored alternation", Matcher{"env", OpRegex, "a|b"}, map[string]string{"env": "ab"}, false},
		{"multiline cannot unanchor", Matcher{"env", OpRegex, "(?m)^a$"}, map[string]string{"env": "b\na"}, false},
		{"dot spans newline", Matcher{"env", OpRegex, "a.b"}, map[string]string{"env": "a\nb"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rs := mustCompile(t, []Rule{{
				Matchers: []Matcher{tc.matcher},
				Write:    map[string]string{"hit": "yes"},
			}})
			labels := maps.Clone(tc.labels)
			require.Equal(t, tc.want, rs.Apply(labels), "labels are now %v", labels)
			_, ok := labels["hit"]
			require.Equal(t, tc.want, ok)
		})
	}
}

func TestMatchersAreANDed(t *testing.T) {
	rs := mustCompile(t, []Rule{{
		Matchers: []Matcher{
			{"env", OpEqual, "prod"},
			{"severity", OpRegex, "crit.*"},
			{"noise", OpNotEqual, "true"},
		},
		Write: map[string]string{"team": "sre"},
	}})

	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{"env": "prod", "severity": "critical"}, true},
		{map[string]string{"env": "dev", "severity": "critical"}, false},
		{map[string]string{"env": "prod", "severity": "warning"}, false},
		{map[string]string{"env": "prod", "severity": "critical", "noise": "true"}, false},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, rs.Apply(maps.Clone(tc.labels)), "labels %v", tc.labels)
	}
}

func TestNoMatchersMatchesEverything(t *testing.T) {
	rs := mustCompile(t, []Rule{{Write: map[string]string{"source": "alertmanager"}}})
	labels := map[string]string{}
	require.True(t, rs.Apply(labels))
	require.Equal(t, "alertmanager", labels["source"])
}

func TestAggregation(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]string
		writes   []string
		want     string
	}{
		{"single write", nil, []string{"a"}, "a"},
		{"two rules aggregate", nil, []string{"a", "b"}, "a b"},
		{"aggregation is sorted", nil, []string{"c", "a", "b"}, "a b c"},
		{"duplicate values deduped", nil, []string{"a", "a"}, "a"},
		{"multi-token value", nil, []string{"b a"}, "a b"},
		{"multi-token dedup across rules", nil, []string{"a b", "b c"}, "a b c"},
		{"merges with existing", map[string]string{"team": "x"}, []string{"a"}, "a x"},
		{"merges and dedups existing", map[string]string{"team": "b"}, []string{"a b"}, "a b"},
		{"existing empty string", map[string]string{"team": ""}, []string{"a"}, "a"},
		{"existing whitespace normalized", map[string]string{"team": "  b   a "}, []string{"c"}, "a b c"},
		{"tabs and newlines are separators", nil, []string{"b\ta\nc"}, "a b c"},
		{"many rules", nil, []string{"d", "b a", "c", "a", "e d"}, "a b c d e"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules := make([]Rule, len(tc.writes))
			for i, w := range tc.writes {
				rules[i] = Rule{
					Matchers: []Matcher{{"env", OpEqual, "prod"}},
					Write:    map[string]string{"team": w},
				}
			}
			rs := mustCompile(t, rules)

			labels := map[string]string{"env": "prod"}
			maps.Copy(labels, tc.existing)
			rs.Apply(labels)
			require.Equal(t, tc.want, labels["team"])
		})
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	rs := mustCompile(t, []Rule{
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "sre core"}},
		{Matchers: []Matcher{{"severity", OpEqual, "critical"}}, Write: map[string]string{"team": "oncall", "page": "yes"}},
	})

	labels := map[string]string{"env": "prod", "severity": "critical", "team": "existing"}
	require.True(t, rs.Apply(labels), "first Apply should report a change")
	first := maps.Clone(labels)
	require.Equal(t, "core existing oncall sre", labels["team"])

	require.False(t, rs.Apply(labels), "second Apply should report no change")
	require.Equal(t, first, labels)
}

// The central guarantee: matching reads only the original label set, so
// permuting the rules cannot change the outcome.
func TestRuleOrderDoesNotMatter(t *testing.T) {
	rules := []Rule{
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "sre", "tier": "1"}},
		{Matchers: []Matcher{{"team", OpEqual, "sre"}}, Write: map[string]string{"trap": "matched on a written value"}},
		{Matchers: []Matcher{{"severity", OpRegex, "crit.*|warn.*"}}, Write: map[string]string{"team": "oncall"}},
		{Matchers: []Matcher{{"tier", OpNotEqual, "1"}}, Write: map[string]string{"team": "triage"}},
		{Write: map[string]string{"seen": "true"}},
		{Matchers: []Matcher{{"region", OpNotRegex, "us-.*"}}, Write: map[string]string{"team": "intl"}},
	}
	input := map[string]string{"env": "prod", "severity": "critical", "region": "eu-west-1"}

	base := maps.Clone(input)
	mustCompile(t, rules).Apply(base)

	// No rule may observe another rule's write: the trap rule matches
	// team="sre", which rule 0 writes, and must not fire.
	require.NotContains(t, base, "trap", "a rule matched a value written by another rule")
	// tier!="1" must match, because tier is absent from the *original* labels
	// even though rule 0 goes on to write it.
	require.Contains(t, base["team"], "triage")

	rng := rand.New(rand.NewSource(1))
	for i := range 200 {
		shuffled := slicesClone(rules)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })

		got := maps.Clone(input)
		mustCompile(t, shuffled).Apply(got)
		require.Equal(t, base, got, "permutation %d", i)
	}
}

func slicesClone(r []Rule) []Rule {
	out := make([]Rule, len(r))
	copy(out, r)
	return out
}

func TestApplyReportsChange(t *testing.T) {
	rs := mustCompile(t, []Rule{{
		Matchers: []Matcher{{"env", OpEqual, "prod"}},
		Write:    map[string]string{"team": "sre"},
	}})

	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"no rule matches", map[string]string{"env": "dev"}, false},
		{"label added", map[string]string{"env": "prod"}, true},
		{"label already correct", map[string]string{"env": "prod", "team": "sre"}, false},
		{"label needs merge", map[string]string{"env": "prod", "team": "dba"}, true},
		{"label already merged", map[string]string{"env": "prod", "team": "dba sre"}, false},
		{"existing needs normalizing", map[string]string{"env": "prod", "team": "sre  dba"}, true},
		{"nil map", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := maps.Clone(tc.labels)
			got := rs.Apply(tc.labels)
			require.Equal(t, tc.want, got)
			if !got {
				require.Equal(t, before, tc.labels, "reported no change but mutated the map")
			}
		})
	}
}

func TestMultipleLabelsWritten(t *testing.T) {
	rs := mustCompile(t, []Rule{
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "sre", "page": "yes", "tier": "1"}},
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "dba", "tier": "0"}},
	})
	labels := map[string]string{"env": "prod"}
	rs.Apply(labels)

	require.Equal(t, map[string]string{
		"env": "prod", "team": "dba sre", "page": "yes", "tier": "0 1",
	}, labels)
}

func TestMatched(t *testing.T) {
	rs := mustCompile(t, []Rule{
		{Name: "a", Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"x": "1"}},
		{Name: "b", Matchers: []Matcher{{"env", OpEqual, "dev"}}, Write: map[string]string{"x": "2"}},
		{Name: "c", Write: map[string]string{"x": "3"}},
	})
	require.ElementsMatch(t, []int{0, 2}, rs.Matched(map[string]string{"env": "prod"}))
	require.ElementsMatch(t, []int{1, 2}, rs.Matched(map[string]string{"env": "dev"}))
	require.ElementsMatch(t, []int{2}, rs.Matched(map[string]string{}))
	require.Equal(t, 3, rs.Len())
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		want string
	}{
		{"empty write", Rule{Matchers: []Matcher{{"a", OpEqual, "b"}}}, "at least one label"},
		{"empty write key", Rule{Write: map[string]string{"": "v"}}, "empty label name"},
		{"empty write value", Rule{Write: map[string]string{"k": ""}}, "no value"},
		{"whitespace write value", Rule{Write: map[string]string{"k": "   "}}, "no value"},
		{"empty matcher name", Rule{Matchers: []Matcher{{"", OpEqual, "b"}}, Write: map[string]string{"k": "v"}}, "empty label name"},
		{"unknown operator", Rule{Matchers: []Matcher{{"a", Op("~="), "b"}}, Write: map[string]string{"k": "v"}}, `unknown operator "~="`},
		{"bad regex", Rule{Matchers: []Matcher{{"a", OpRegex, "("}}, Write: map[string]string{"k": "v"}}, "error parsing regexp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.rule.Name = "the-rule"
			_, err := Compile([]Rule{{Write: map[string]string{"ok": "v"}}, tc.rule})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)

			var re *RuleError
			require.True(t, errors.As(err, &re), "error %v is not a *RuleError", err)
			require.Equal(t, 1, re.Index)
			require.Equal(t, "the-rule", re.Name)
		})
	}
}

// Rules carry JSON tags so a rule set can be authored as a config file. Each
// operator must survive the round trip and mean the same thing afterwards.
func TestRulesRoundTripThroughJSON(t *testing.T) {
	const config = `[
	  {
	    "name": "page-on-prod-outage",
	    "matchers": [
	      {"name": "env",      "op": "=",  "value": "prod"},
	      {"name": "severity", "op": "=~", "value": "critical|fatal"},
	      {"name": "silenced", "op": "!=", "value": "true"},
	      {"name": "team",     "op": "!~", "value": "test-.*"}
	    ],
	    "write": {"team": "sre", "page": "yes"}
	  }
	]`

	want := []Rule{{
		Name: "page-on-prod-outage",
		Matchers: []Matcher{
			{"env", OpEqual, "prod"},
			{"severity", OpRegex, "critical|fatal"},
			{"silenced", OpNotEqual, "true"},
			{"team", OpNotRegex, "test-.*"},
		},
		Write: map[string]string{"team": "sre", "page": "yes"},
	}}

	var rules []Rule
	require.NoError(t, json.Unmarshal([]byte(config), &rules))
	require.Equal(t, want, rules)

	// Marshaling back and re-decoding must be lossless.
	encoded, err := json.Marshal(rules)
	require.NoError(t, err)
	var again []Rule
	require.NoError(t, json.Unmarshal(encoded, &again))
	require.Equal(t, want, again)

	// The decoded rules behave as written.
	labels := map[string]string{"env": "prod", "severity": "fatal"}
	require.True(t, mustCompile(t, rules).Apply(labels))
	require.Equal(t, map[string]string{
		"env": "prod", "severity": "fatal", "team": "sre", "page": "yes",
	}, labels)
}

func TestCompileEmpty(t *testing.T) {
	rs := mustCompile(t, nil)
	labels := map[string]string{"a": "b"}
	require.False(t, rs.Apply(labels))
	require.Equal(t, 0, rs.Len())

	var nilSet *RuleSet
	require.False(t, nilSet.Apply(labels))
	require.Equal(t, 0, nilSet.Len())
	require.Nil(t, nilSet.Matched(labels))
}

// referenceMatch is an independent, deliberately naive evaluator used as an
// oracle. It shares no code with Compile, the index, or fastRegex.
func referenceMatch(r Rule, labels map[string]string) bool {
	for _, m := range r.Matchers {
		v := labels[m.Name]
		var ok bool
		switch m.Op {
		case OpEqual:
			ok = v == m.Value
		case OpNotEqual:
			ok = v != m.Value
		case OpRegex:
			ok = regexp.MustCompile(`\A(?s:` + m.Value + `)\z`).MatchString(v)
		case OpNotRegex:
			ok = !regexp.MustCompile(`\A(?s:` + m.Value + `)\z`).MatchString(v)
		}
		if !ok {
			return false
		}
	}
	return true
}

// The index and the matcher rewrites must never change which rules match,
// only how quickly they are found.
func TestMatchingAgreesWithReference(t *testing.T) {
	rules := []Rule{
		{Matchers: []Matcher{{"env", OpEqual, "prod"}, {"region", OpEqual, "us-east-1"}}, Write: map[string]string{"o": "a"}},
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"o": "b"}},
		{Matchers: []Matcher{{"env", OpEqual, "dev"}, {"team", OpRegex, "s.e"}}, Write: map[string]string{"o": "c"}},
		{Matchers: []Matcher{{"env", OpEqual, ""}}, Write: map[string]string{"o": "d"}},
		{Matchers: []Matcher{{"region", OpNotEqual, "eu-west-1"}}, Write: map[string]string{"o": "e"}},
		// A =~ that reduces to a literal, so it becomes indexable.
		{Matchers: []Matcher{{"env", OpRegex, "prod"}, {"tier", OpEqual, "1"}}, Write: map[string]string{"o": "f"}},
		// Contradictory matchers: can never match.
		{Matchers: []Matcher{{"env", OpEqual, "prod"}, {"env", OpEqual, "dev"}}, Write: map[string]string{"o": "g"}},
		// Unconditional.
		{Write: map[string]string{"o": "h"}},
		// Set-of-literals regex, and its negation.
		{Matchers: []Matcher{{"env", OpRegex, "prod|dev"}}, Write: map[string]string{"o": "i"}},
		{Matchers: []Matcher{{"team", OpNotRegex, "sre|dba"}}, Write: map[string]string{"o": "j"}},
		// Genuine regex requiring the engine.
		{Matchers: []Matcher{{"region", OpRegex, "[a-z]+-[a-z]+-[0-9]+"}}, Write: map[string]string{"o": "k"}},
		{Matchers: []Matcher{{"env", OpEqual, "prod"}, {"tier", OpNotEqual, ""}}, Write: map[string]string{"o": "l"}},
	}
	rs := mustCompile(t, rules)
	unindexed, err := compile(rules, false)
	require.NoError(t, err)
	require.NotEmpty(t, rs.index, "the index should have been built")

	envs := []string{"prod", "dev", "", "staging"}
	regions := []string{"us-east-1", "eu-west-1", "eu", ""}
	teams := []string{"sre", "she", "dba", "x", ""}
	tiers := []string{"1", "2", ""}

	for _, env := range envs {
		for _, region := range regions {
			for _, team := range teams {
				for _, tier := range tiers {
					labels := map[string]string{}
					for k, v := range map[string]string{"env": env, "region": region, "team": team, "tier": tier} {
						if v != "" {
							labels[k] = v
						}
					}
					var want []int
					for i := range rules {
						if referenceMatch(rules[i], labels) {
							want = append(want, i)
						}
					}
					require.ElementsMatch(t, want, rs.Matched(labels), "indexed, labels %v", labels)
					require.ElementsMatch(t, want, unindexed.Matched(labels), "unindexed, labels %v", labels)
				}
			}
		}
	}
}

func TestConcurrentApply(t *testing.T) {
	rs := mustCompile(t, []Rule{
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "sre"}},
		{Matchers: []Matcher{{"severity", OpRegex, "crit.*"}}, Write: map[string]string{"team": "oncall", "page": "yes"}},
		{Write: map[string]string{"seen": "true"}},
	})

	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				labels := map[string]string{
					"env":      "prod",
					"severity": "critical",
					"instance": fmt.Sprintf("host-%d-%d", g, i),
				}
				rs.Apply(labels)
				if labels["team"] != "oncall sre" || labels["page"] != "yes" || labels["seen"] != "true" {
					t.Errorf("goroutine %d iteration %d: %v", g, i, labels)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Apply must not allocate once the scratch pool is warm, on either the
// no-match path or the already-canonical path.
func TestApplyDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector instruments allocation")
	}
	rs := mustCompile(t, []Rule{
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "sre"}},
		{Matchers: []Matcher{{"env", OpEqual, "prod"}}, Write: map[string]string{"team": "dba"}},
		{Matchers: []Matcher{{"severity", OpRegex, "crit.*|warn.*"}}, Write: map[string]string{"page": "yes"}},
	})

	t.Run("no match", func(t *testing.T) {
		labels := map[string]string{"env": "dev", "severity": "info", "job": "api"}
		require.Zero(t, testing.AllocsPerRun(200, func() { rs.Apply(labels) }))
	})

	t.Run("already canonical", func(t *testing.T) {
		labels := map[string]string{"env": "prod", "severity": "critical", "team": "dba sre", "page": "yes"}
		require.Zero(t, testing.AllocsPerRun(200, func() { rs.Apply(labels) }))
	})

	t.Run("single contributor on absent label", func(t *testing.T) {
		labels := map[string]string{"env": "dev", "severity": "critical"}
		require.Zero(t, testing.AllocsPerRun(200, func() {
			delete(labels, "page")
			rs.Apply(labels)
		}))
	})
}

func TestScratchIsNotPinnedToLabels(t *testing.T) {
	rs := mustCompile(t, []Rule{{Write: map[string]string{"team": "sre"}}})
	labels := map[string]string{"team": strings.Repeat("x", 32)}
	rs.Apply(labels)

	s := getScratch()
	defer putScratch(s)
	for _, a := range s.accums {
		require.Empty(t, a.key, "pooled scratch still references a label name")
		for _, tok := range a.tokens[:cap(a.tokens)] {
			require.Empty(t, tok, "pooled scratch still references a label value")
		}
	}
}
