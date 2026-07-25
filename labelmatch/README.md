# `labelmatch`

Rule-based label set matching and mutation, in the style of Alertmanager
matchers.

A `Rule` pairs matchers with labels to write. `Compile` turns rules into an
immutable, concurrency-safe `RuleSet`; `Apply` evaluates every rule against a
label set and then mutates it in a single final pass.

```go
import "github.com/pliu/go-utils/labelmatch"

rs, err := labelmatch.Compile([]labelmatch.Rule{
    {
        Name: "prod-critical-pages-sre",
        Matchers: []labelmatch.Matcher{
            {Name: "env", Op: labelmatch.OpEqual, Value: "prod"},
            {Name: "severity", Op: labelmatch.OpRegex, Value: "crit.*"},
        },
        Write: map[string]string{"team": "sre", "page": "yes"},
    },
    {
        Name:     "database-alerts-notify-dba",
        Matchers: []labelmatch.Matcher{{Name: "job", Op: labelmatch.OpEqual, Value: "postgres"}},
        Write:    map[string]string{"team": "dba"},
    },
})
if err != nil {
    log.Fatal(err) // bad rules: fail fast at startup, not per alert
}

labels := map[string]string{"env": "prod", "severity": "critical", "job": "postgres"}
changed := rs.Apply(labels)
// labels: env=prod severity=critical job=postgres team="dba sre" page=yes
```

## Matching

| Operator | Constant | Meaning |
|---|---|---|
| `=` | `OpEqual` | label value equals `Value` |
| `!=` | `OpNotEqual` | label value differs from `Value` |
| `=~` | `OpRegex` | label value fully matches the regex |
| `!~` | `OpNotRegex` | label value does not fully match the regex |

- All of a rule's matchers must match (AND). A rule with no matchers matches
  every label set, which is useful for unconditional enrichment.
- A label absent from the set is treated as the empty string, as in
  Alertmanager. So `{env, !=, "prod"}` matches a label set with no `env`, and
  `{env, =, ""}` matches exactly those sets.
- Regexes are anchored on both ends: `=~ "prod"` does not match
  `production`. An embedded `(?m)` cannot unanchor the pattern.

## Rule order does not matter

Matching and mutation are strictly separated. Every matcher is evaluated
against the original, unmodified label set, and no write is visible to any
matcher — so a rule can never observe another rule's output, and permuting
the slice passed to `Compile` cannot change the result.

Writes from different rules to the same label are **aggregated** rather than
treated as a conflict. Values are tokenized on whitespace, merged with the
tokens already present in the label, deduplicated, sorted, and rejoined with
single spaces:

```
labels: {"team": "x"}
rule A: write {"team": "a"}
rule B: write {"team": "b x"}
result: {"team": "a b x"}
```

Sorting is what makes aggregation order-independent, and merging with the
existing value makes `Apply` idempotent: applying the same `RuleSet` twice
produces the same result, and the second call returns `false` for "changed".

`Apply` does not copy the map and does not synchronize access to it, so the
caller must not read or write it concurrently. Applying a `RuleSet` from many
goroutines to *different* maps is safe.

## Rules as configuration

`Rule` and `Matcher` carry JSON tags, so a rule set can live in a config file:

```json
{
  "rules": [
    {
      "name": "page-on-prod-outage",
      "matchers": [
        {"name": "env",      "op": "=",  "value": "prod"},
        {"name": "severity", "op": "=~", "value": "critical|fatal"},
        {"name": "silenced", "op": "!=", "value": "true"}
      ],
      "write": {"team": "sre", "page": "yes"}
    }
  ]
}
```

`Compile` rejects a rule with an empty `write`, an empty label name or value,
an unknown operator, or an invalid regex, returning a `*RuleError` that
identifies the offending rule by index and `name`.

### Hot reloading

A compiled `RuleSet` is immutable, so swapping one in is a single pointer
store. Compiling inside a `Validate` method — which
[`configmanager`](../configmanager) runs on the decoded instance *before*
promoting it — stashes the `RuleSet` on that same instance, so the manager's
own atomic swap carries the compiled rules:

```go
type ruleConfig struct {
    Rules    []labelmatch.Rule `json:"rules"`
    compiled *labelmatch.RuleSet
}

func (c *ruleConfig) Validate() error {
    rs, err := labelmatch.Compile(c.Rules)
    if err != nil {
        return err
    }
    c.compiled = rs
    return nil
}

mgr, err := configmanager.New[ruleConfig](path)
...
mgr.Get().compiled.Apply(labels)
```

This needs no `WithOnSwap` callback and no separate `atomic.Pointer`, compiles
each version exactly once, and is populated from the very first load, so there
is no window where the rule set is missing. Config and rules can never skew,
because they are the same object. A file whose rules do not compile is
rejected before promotion, so the previously compiled rules keep serving and
the failure is reported through `mgr.Err()`.

Read the config with `Get`, which returns the validated instance.
`GetDeepCopy` round-trips through JSON and cannot carry an unexported field,
so the compiled rules would come back nil — and since `Apply` is nil-safe,
that fails silently. Guard the accessor if that is a risk. See
`Example_hotReload` for a complete, runnable version.

## Performance

`Compile` does everything that does not depend on the label set, so `Apply`
stays allocation-free on a warm process:

- **Selectivity index.** Each rule is indexed by its rarest non-empty
  equality matcher, so `Apply` only evaluates rules that can possibly match
  instead of scanning all of them. That matcher is then dropped from the
  rule, since reaching the rule already proves it holds. Rules with no
  indexable matcher are checked on every call.
- **Regex reduction.** Because patterns are fully anchored, one whose
  language is a finite set of literals is equivalent to set membership.
  `critical` becomes a string comparison (and is rewritten into an equality
  matcher, which makes the rule indexable); `critical|warning|info` becomes a
  map lookup. Only genuinely unbounded patterns reach the regexp engine.
- **Cheapest-first matchers.** Within a rule, string comparisons run before
  set lookups, which run before the regexp engine, so expensive tests are
  short-circuited.
- **Pooled scratch space.** Match lists, per-label accumulators and the join
  buffer are reused across calls; tokenizing reslices the input rather than
  copying it. `Apply` allocates only the final label strings, and only for
  labels whose value actually changes.

On an M2 Pro, with a realistic rule set and an 8-label alert:

```
BenchmarkApply/rules=10                  344.9 ns/op    0 B/op   0 allocs/op
BenchmarkApply/rules=100                 348.6 ns/op    0 B/op   0 allocs/op
BenchmarkApply/rules=1000                398.9 ns/op    0 B/op   0 allocs/op
BenchmarkApply/rules=10000               402.0 ns/op    0 B/op   0 allocs/op
BenchmarkApplyIdempotent                 222.1 ns/op    0 B/op   0 allocs/op
BenchmarkApplyNoMatch                    102.9 ns/op    0 B/op   0 allocs/op
BenchmarkApplyParallel                    58.5 ns/op    0 B/op   0 allocs/op
```

Matching a label set costs about the same against 10 rules as against 10000,
because the index means only the handful of rules that can possibly match are
ever evaluated. `BenchmarkApplyWithoutIndex` compiles the same rules with the
index disabled for comparison, and scales linearly instead:

```
BenchmarkApplyWithoutIndex/rules=10      396.5 ns/op    0 B/op   0 allocs/op
BenchmarkApplyWithoutIndex/rules=100    1370   ns/op    0 B/op   0 allocs/op
BenchmarkApplyWithoutIndex/rules=1000  13446   ns/op    0 B/op   0 allocs/op
BenchmarkApplyWithoutIndex/rules=10000 119417  ns/op    0 B/op   0 allocs/op
```

The regex reduction shows up the same way: 2.9 ns for a pattern that collapses
to a string comparison and 8.1 ns for one that collapses to a set lookup,
against 73.1 ns for one that needs the engine.

`Compile` is the expensive half, at ~1.2 ms for 1000 rules, which is why it
happens once at startup or on config reload rather than per label set.
