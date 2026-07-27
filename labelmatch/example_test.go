package labelmatch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pliu/go-utils/configmanager"
	"github.com/pliu/go-utils/labelmatch"
)

// prometheusAlert contains the part of a Prometheus alert used by this
// handler. Fields such as annotations and startsAt can be added as needed;
// unknown fields in the request are ignored by encoding/json.
type prometheusAlert struct {
	Labels map[string]string `json:"labels"`
}

// newAlertHandler builds a handler around a RuleSet compiled at startup.
// Decoding labels directly into map[string]string lets Apply enrich the
// request in place, without converting or copying each label set.
func newAlertHandler(rules *labelmatch.RuleSet, consume func([]prometheusAlert)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var alerts []prometheusAlert
		if err := json.NewDecoder(r.Body).Decode(&alerts); err != nil {
			http.Error(w, "invalid alerts payload", http.StatusBadRequest)
			return
		}

		for i := range alerts {
			rules.Apply(alerts[i].Labels)
		}
		consume(alerts)
		w.WriteHeader(http.StatusNoContent)
	})
}

// Prometheus sends Alertmanager a JSON list whose alerts contain a labels
// object. A handler can decode that object into the map type Apply expects and
// reuse one concurrency-safe RuleSet for every request.
func Example_alertHandler() {
	rules, err := labelmatch.Compile([]labelmatch.Rule{{
		Matchers: []labelmatch.Matcher{
			{Name: "env", Op: labelmatch.OpEqual, Value: "prod"},
			{Name: "severity", Op: labelmatch.OpRegex, Value: "critical|warning"},
		},
		Write: map[string]string{"team": "sre", "page": "yes"},
	}})
	if err != nil {
		log.Fatal(err)
	}

	handler := newAlertHandler(rules, func(alerts []prometheusAlert) {
		// Forward, store, or otherwise process the enriched alerts here.
		for _, alert := range alerts {
			fmt.Printf("%s: team=%q page=%q\n",
				alert.Labels["alertname"], alert.Labels["team"], alert.Labels["page"])
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts", bytes.NewBufferString(`[
		{"labels":{"alertname":"HighErrorRate","env":"prod","severity":"critical"}},
		{"labels":{"alertname":"QueueBacklog","env":"staging","severity":"warning"}}
	]`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println("status:", rec.Code)

	// Output:
	// HighErrorRate: team="sre" page="yes"
	// QueueBacklog: team="" page=""
	// status: 204
}

func benchmarkAlertPayload(b *testing.B, size int) []byte {
	b.Helper()
	alerts := make([]prometheusAlert, size)
	for i := range alerts {
		alerts[i].Labels = map[string]string{
			"alertname": fmt.Sprintf("Alert%d", i),
			"env":       []string{"prod", "staging"}[i%2],
			"severity":  []string{"critical", "info"}[i%2],
			"service":   fmt.Sprintf("service-%d", i),
			"instance":  fmt.Sprintf("10.0.0.%d:9090", i),
		}
	}
	payload, err := json.Marshal(alerts)
	if err != nil {
		b.Fatal(err)
	}
	return payload
}

func benchmarkAlertHandler(b *testing.B) http.Handler {
	b.Helper()
	rules, err := labelmatch.Compile([]labelmatch.Rule{{
		Matchers: []labelmatch.Matcher{
			{Name: "env", Op: labelmatch.OpEqual, Value: "prod"},
			{Name: "severity", Op: labelmatch.OpRegex, Value: "critical|warning"},
		},
		Write: map[string]string{"team": "sre", "page": "yes"},
	}})
	if err != nil {
		b.Fatal(err)
	}
	return newAlertHandler(rules, func([]prometheusAlert) {})
}

// BenchmarkAlertHandler measures JSON decoding and matching for typical batch
// sizes. Rule compilation deliberately stays outside the request path.
func BenchmarkAlertHandler(b *testing.B) {
	for _, size := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("alerts=%d", size), func(b *testing.B) {
			handler := benchmarkAlertHandler(b)
			payload := benchmarkAlertPayload(b, size)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts", bytes.NewReader(payload))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusNoContent {
					b.Fatalf("status = %d", rec.Code)
				}
			}
		})
	}
}

// BenchmarkAlertHandlerParallel exercises the same RuleSet from concurrent
// requests; each decoder owns the maps that its request passes to Apply.
func BenchmarkAlertHandlerParallel(b *testing.B) {
	handler := benchmarkAlertHandler(b)
	payload := benchmarkAlertPayload(b, 100)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts", bytes.NewReader(payload))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				b.Errorf("status = %d", rec.Code)
			}
		}
	})
}

// ruleConfig is a config file holding a rule set. Compiling inside Validate,
// which configmanager runs on the decoded instance before promoting it,
// stashes the compiled RuleSet on that same instance — so the manager's own
// atomic swap carries the compiled rules, with no extra wiring and no second
// compile.
type ruleConfig struct {
	Rules    []labelmatch.Rule `json:"rules"`
	compiled *labelmatch.RuleSet
}

// Validate compiles the rules and keeps the result. Returning an error here
// rejects the file before it is promoted, so rules that do not compile never
// take effect and the previously compiled rules keep serving.
func (c *ruleConfig) Validate() error {
	rs, err := labelmatch.Compile(c.Rules)
	if err != nil {
		return err
	}
	c.compiled = rs
	return nil
}

// RuleSet returns the rules compiled during validation. Read it from
// Manager.Get, which returns the validated instance; Manager.GetDeepCopy
// round-trips through JSON and cannot carry an unexported field.
func (c *ruleConfig) RuleSet() *labelmatch.RuleSet {
	if c.compiled == nil {
		panic("ruleConfig was not validated (GetDeepCopy?)")
	}
	return c.compiled
}

// Rules can live in a config file and be recompiled whenever it changes.
func Example_hotReload() {
	dir, err := os.MkdirTemp("", "labelmatch-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rules.json")

	writeRules := func(team string) {
		rules := fmt.Sprintf(
			`{"rules":[{"matchers":[{"name":"env","op":"=","value":"prod"}],"write":{"team":%q}}]}`,
			team)
		if err := os.WriteFile(path, []byte(rules), 0o600); err != nil {
			log.Fatal(err)
		}
	}
	writeRules("platform")

	mgr, err := configmanager.New[ruleConfig](path,
		configmanager.WithPollInterval[ruleConfig](10*time.Millisecond))
	if err != nil {
		log.Fatal(err)
	}
	defer mgr.Close()

	// The compiled rules ride along with the config, so there is nothing to
	// seed and no window where the rule set is missing.
	team := func() string {
		labels := map[string]string{"env": "prod"}
		mgr.Get().RuleSet().Apply(labels)
		return labels["team"]
	}
	fmt.Println("initial:", team())

	// A changed file is recompiled and swapped in atomically.
	writeRules("sre")
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && team() == "platform"; {
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Println("after reload:", team())

	// A file whose rules do not compile is rejected by Validate before
	// promotion, so the rules above stay in effect.
	if err := os.WriteFile(path, []byte(
		`{"rules":[{"matchers":[{"name":"env","op":"??","value":"prod"}],"write":{"team":"broken"}}]}`,
	), 0o600); err != nil {
		log.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && mgr.Err() == nil; {
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Println("after bad rules:", team())
	fmt.Println("error reported:", mgr.Err() != nil)

	// Output:
	// initial: platform
	// after reload: sre
	// after bad rules: sre
	// error reported: true
}
