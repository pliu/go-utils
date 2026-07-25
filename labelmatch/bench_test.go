package labelmatch

import (
	"fmt"
	"maps"
	"testing"
)

// benchRules builds n rules of a realistic shape: most are selected by an
// equality matcher on a high-cardinality label, with a few regex and
// unconditional rules mixed in.
func benchRules(n int) []Rule {
	rules := make([]Rule, 0, n)
	for i := range n {
		switch i % 8 {
		case 0:
			rules = append(rules, Rule{
				Matchers: []Matcher{
					{"service", OpEqual, fmt.Sprintf("svc-%d", i)},
					{"env", OpEqual, "prod"},
				},
				Write: map[string]string{"team": fmt.Sprintf("team-%d", i%16)},
			})
		case 1:
			rules = append(rules, Rule{
				Matchers: []Matcher{
					{"service", OpEqual, fmt.Sprintf("svc-%d", i)},
					{"severity", OpRegex, "crit.*|warn.*"},
				},
				Write: map[string]string{"page": "yes"},
			})
		case 2:
			rules = append(rules, Rule{
				Matchers: []Matcher{{"region", OpEqual, fmt.Sprintf("r-%d", i%32)}},
				Write:    map[string]string{"zone": fmt.Sprintf("z-%d", i%32)},
			})
		default:
			rules = append(rules, Rule{
				Matchers: []Matcher{
					{"service", OpEqual, fmt.Sprintf("svc-%d", i)},
					{"instance", OpNotEqual, ""},
				},
				Write: map[string]string{"owner": fmt.Sprintf("owner-%d", i%16)},
			})
		}
	}
	// A handful of rules that must be checked on every call.
	rules = append(rules,
		Rule{Write: map[string]string{"pipeline": "v2"}},
		Rule{Matchers: []Matcher{{"severity", OpRegex, "crit.*"}}, Write: map[string]string{"team": "oncall"}},
	)
	return rules
}

func benchLabels() map[string]string {
	return map[string]string{
		"alertname": "HighLatency",
		"service":   "svc-500",
		"env":       "prod",
		"severity":  "critical",
		"region":    "r-4",
		"instance":  "10.0.14.22:9100",
		"job":       "node",
		"cluster":   "eu-west-1a",
	}
}

func BenchmarkApply(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			rs, err := Compile(benchRules(n))
			if err != nil {
				b.Fatal(err)
			}
			src := benchLabels()
			labels := maps.Clone(src)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				clear(labels)
				maps.Copy(labels, src)
				rs.Apply(labels)
			}
		})
	}
}

// Apply on a label set that is already in its final state: the common case
// for a re-evaluated alert, and the one that should not allocate at all.
func BenchmarkApplyIdempotent(b *testing.B) {
	rs, err := Compile(benchRules(1000))
	if err != nil {
		b.Fatal(err)
	}
	labels := benchLabels()
	rs.Apply(labels)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rs.Apply(labels)
	}
}

// Apply where no rule matches: dominated by the index probe.
func BenchmarkApplyNoMatch(b *testing.B) {
	rs, err := Compile(benchRules(1000))
	if err != nil {
		b.Fatal(err)
	}
	labels := map[string]string{
		"alertname": "Unknown",
		"service":   "not-a-service",
		"env":       "sandbox",
		"severity":  "info",
		"region":    "nowhere",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rs.Apply(labels)
	}
}

func BenchmarkApplyParallel(b *testing.B) {
	rs, err := Compile(benchRules(1000))
	if err != nil {
		b.Fatal(err)
	}
	src := benchLabels()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		labels := maps.Clone(src)
		for pb.Next() {
			clear(labels)
			maps.Copy(labels, src)
			rs.Apply(labels)
		}
	})
}

// The index exists to avoid a linear scan; this measures what it saves.
func BenchmarkApplyWithoutIndex(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			rs, err := compile(benchRules(n), false)
			if err != nil {
				b.Fatal(err)
			}
			src := benchLabels()
			labels := maps.Clone(src)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				clear(labels)
				maps.Copy(labels, src)
				rs.Apply(labels)
			}
		})
	}
}

func BenchmarkCompile(b *testing.B) {
	rules := benchRules(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Compile(rules); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFastRegex(b *testing.B) {
	cases := []struct{ name, pattern, value string }{
		{"literal", "critical", "critical"},
		{"set", "critical|warning|info", "warning"},
		{"engine", "crit.*|warn.*", "warning"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			re, err := compileRegex(tc.pattern)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				re.matchString(tc.value)
			}
		})
	}
}
