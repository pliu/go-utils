package labelmatch

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every pattern here must behave exactly like the equivalent anchored
// regexp, whichever fast path it takes.
func TestFastRegexMatchesStdlib(t *testing.T) {
	patterns := []string{
		"", "a", "abc", "a|b", "a|b|c", "prod|dev|staging",
		"foo.*", ".*", ".+", "a.b", "[ab]", "[a-c]", "[a-c]x", "x[a-c]y",
		"(a|b)(c|d)", "(?:a|b)c", "a(b|c)d", "abc|", "|abc",
		"[a-z]+-[0-9]+", "us-(east|west)-[12]", `\d+`, `\w`, "(?i)abc",
		"a{2}", "a{2,3}", "a?", "(a)", "((a|b))", "x(?:)y",
		"^a", "a$", `\Aa\z`, "(?m)^a$", `\bfoo\b`, "[^a]",
		"prod", "prod.*", ".*prod.*",
	}
	values := []string{
		"", "a", "b", "c", "ab", "abc", "abcd", "ac", "ad", "bc", "bd",
		"prod", "dev", "staging", "production", "x", "xy", "xay", "xby",
		"us-east-1", "us-west-2", "eu-west-1", "foo", "a\nb", "aa", "aaa",
		"ABC", "1", "12", "a-1", "z-99", "_",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			fast, err := compileRegex(pattern)
			require.NoError(t, err)
			want := regexp.MustCompile(`\A(?s:` + pattern + `)\z`)

			for _, v := range values {
				require.Equalf(t, want.MatchString(v), fast.matchString(v),
					"pattern %q value %q (kind %d)", pattern, v, fast.kind)
			}
		})
	}
}

// The optimization only pays off if it actually fires on common patterns.
func TestRegexKindSelection(t *testing.T) {
	tests := []struct {
		pattern string
		want    regexKind
	}{
		{"", kindLiteral},
		{"prod", kindLiteral},
		{"(prod)", kindLiteral},
		{"a|a", kindLiteral},
		{"prod|dev", kindSet},
		{"prod|dev|staging", kindSet},
		{"[abc]", kindSet},
		{"us-(east|west)-[12]", kindSet},
		{"(?i)prod", kindRegex},           // case folding is not a literal set
		{"prod.*", kindRegex},             // unbounded
		{`\d+`, kindRegex},                // unbounded
		{"^prod", kindRegex},              // anchors are not literals
		{"[a-z]", kindSet},                // small enough to enumerate
		{`[\x{0}-\x{10FFFF}]`, kindRegex}, // too large to enumerate
	}
	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			f, err := compileRegex(tc.pattern)
			require.NoError(t, err)
			require.Equal(t, tc.want, f.kind)
		})
	}
}

// A =~ that accepts exactly one value is rewritten to an equality matcher,
// which also makes the rule eligible for indexing.
func TestLiteralRegexBecomesEquality(t *testing.T) {
	rs := mustCompile(t, []Rule{
		{Matchers: []Matcher{{"env", OpRegex, "prod"}}, Write: map[string]string{"a": "1"}},
		{Matchers: []Matcher{{"env", OpNotRegex, "prod"}}, Write: map[string]string{"b": "1"}},
	})
	require.Len(t, rs.index, 1, "the =~ rule should have been indexed")

	labels := map[string]string{"env": "prod"}
	rs.Apply(labels)
	require.Equal(t, map[string]string{"env": "prod", "a": "1"}, labels)

	labels = map[string]string{"env": "dev"}
	rs.Apply(labels)
	require.Equal(t, map[string]string{"env": "dev", "b": "1"}, labels)
}

func TestBadRegexRejected(t *testing.T) {
	_, err := compileRegex("(")
	require.Error(t, err)
}

func TestAppendFields(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{" a ", []string{"a"}},
		{"a b", []string{"a", "b"}},
		{"  a   b  ", []string{"a", "b"}},
		{"a\tb\nc\rd", []string{"a", "b", "c", "d"}},
		{"héllo wörld", []string{"héllo", "wörld"}},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, appendFields(nil, tc.in), "input %q", tc.in)
	}
}

func TestCanonicalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a", "a"},
		{"b a", "a b"},
		{"b a b", "a b"},
		{"  c   a  b ", "a b c"},
		{"a\tb", "a b"},
	}
	for _, tc := range tests {
		got, tokens := canonicalize(tc.in)
		require.Equal(t, tc.want, got, "input %q", tc.in)
		require.Equal(t, got, string(joinInto(nil, tokens)))
	}
}
