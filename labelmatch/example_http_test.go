package labelmatch_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pliu/go-utils/labelmatch"
)

// prometheusAlert contains the part of a Prometheus alert used by this
// handler. Fields such as annotations and startsAt can be added as needed;
// unknown fields in the request are ignored by encoding/json.
type prometheusAlert struct {
	Labels map[string]string `json:"labels"`
}

// maxAlertBytes limits bytes on the wire. Decoded maps occupy more memory, so
// production handlers should choose this limit for their expected alert shape
// and maximum number of concurrent requests.
const maxAlertBytes = 1 << 20 // 1 MiB

// newAlertHandler builds a handler around a RuleSet compiled at startup.
// Decoding labels directly into map[string]string lets Apply enrich the
// request in place, without converting or copying each label set.
func newAlertHandler(rules *labelmatch.RuleSet, consume func([]prometheusAlert)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAlertBytes)
		dec := json.NewDecoder(r.Body)

		var alerts []prometheusAlert
		if err := dec.Decode(&alerts); err != nil {
			writeAlertDecodeError(w, err)
			return
		}
		// Decode must consume the entire body. Decoder.More is insufficient
		// here because a stray trailing ']' or '}' makes More report false.
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeAlertDecodeError(w, err)
			return
		}

		for i := range alerts {
			rules.Apply(alerts[i].Labels)
		}
		consume(alerts)
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeAlertDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		http.Error(w, "alerts payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "invalid alerts payload", http.StatusBadRequest)
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

func TestAlertHandlerRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "trailing data", body: `[{"labels":{"a":"b"}}] trailing`},
		{name: "trailing delimiter", body: `[{"labels":{"a":"b"}}]]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := labelmatch.Compile(nil)
			if err != nil {
				t.Fatal(err)
			}
			consumed := false
			handler := newAlertHandler(rules, func([]prometheusAlert) {
				consumed = true
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts",
				bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if consumed {
				t.Error("invalid payload was consumed")
			}
		})
	}
}

func TestAlertHandlerRejectsOversizedPayload(t *testing.T) {
	rules, err := labelmatch.Compile(nil)
	if err != nil {
		t.Fatal(err)
	}
	consumed := false
	handler := newAlertHandler(rules, func([]prometheusAlert) {
		consumed = true
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts",
		bytes.NewReader(bytes.Repeat([]byte(" "), maxAlertBytes+1)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if consumed {
		t.Error("oversized payload was consumed")
	}
}

const (
	benchmarkRuleCount      = 1000
	benchmarkLabelsPerAlert = 50
)

func benchmarkAlertPayload(b *testing.B, size int) []byte {
	b.Helper()
	alerts := make([]prometheusAlert, size)
	for i := range alerts {
		labels := map[string]string{
			"alertname": fmt.Sprintf("Alert%d", i),
			"env":       []string{"prod", "staging"}[i%2],
			"severity":  []string{"critical", "info"}[i%2],
			"service":   fmt.Sprintf("service-%d", i),
			"instance":  fmt.Sprintf("10.0.0.%d:9090", i),
			"region":    fmt.Sprintf("r-%d", i%32),
			"job":       "node",
			"cluster":   "eu-west-1a",
		}
		for label := range benchmarkLabelsPerAlert - len(labels) {
			labels[fmt.Sprintf("label_%02d", label)] = fmt.Sprintf("value_%02d", label)
		}
		if len(labels) != benchmarkLabelsPerAlert {
			b.Fatalf("alert has %d labels, want %d", len(labels), benchmarkLabelsPerAlert)
		}
		alerts[i].Labels = labels
	}
	payload, err := json.Marshal(alerts)
	if err != nil {
		b.Fatal(err)
	}
	return payload
}

// benchmarkAlertRules builds exactly 1000 rules. Most use a selective service
// or region equality index; the final two exercise rules that must always be
// evaluated.
func benchmarkAlertRules(b *testing.B) *labelmatch.RuleSet {
	b.Helper()
	input := make([]labelmatch.Rule, 0, benchmarkRuleCount)
	for i := range benchmarkRuleCount {
		switch {
		case i == benchmarkRuleCount-2:
			input = append(input, labelmatch.Rule{
				Write: map[string]string{"pipeline": "v2"},
			})
		case i == benchmarkRuleCount-1:
			input = append(input, labelmatch.Rule{
				Matchers: []labelmatch.Matcher{
					{Name: "severity", Op: labelmatch.OpRegex, Value: "crit.*"},
				},
				Write: map[string]string{"team": "oncall"},
			})
		case i%8 == 0:
			input = append(input, labelmatch.Rule{
				Matchers: []labelmatch.Matcher{
					{Name: "service", Op: labelmatch.OpEqual, Value: fmt.Sprintf("service-%d", i)},
					{Name: "env", Op: labelmatch.OpEqual, Value: "prod"},
				},
				Write: map[string]string{"team": fmt.Sprintf("team-%d", i%16)},
			})
		case i%8 == 1:
			input = append(input, labelmatch.Rule{
				Matchers: []labelmatch.Matcher{
					{Name: "service", Op: labelmatch.OpEqual, Value: fmt.Sprintf("service-%d", i)},
					{Name: "severity", Op: labelmatch.OpRegex, Value: "crit.*|warn.*"},
				},
				Write: map[string]string{"page": "yes"},
			})
		case i%8 == 2:
			input = append(input, labelmatch.Rule{
				Matchers: []labelmatch.Matcher{
					{Name: "region", Op: labelmatch.OpEqual, Value: fmt.Sprintf("r-%d", i%32)},
				},
				Write: map[string]string{"zone": fmt.Sprintf("z-%d", i%32)},
			})
		default:
			input = append(input, labelmatch.Rule{
				Matchers: []labelmatch.Matcher{
					{Name: "service", Op: labelmatch.OpEqual, Value: fmt.Sprintf("service-%d", i)},
					{Name: "instance", Op: labelmatch.OpNotEqual, Value: ""},
				},
				Write: map[string]string{"owner": fmt.Sprintf("owner-%d", i%16)},
			})
		}
	}

	rules, err := labelmatch.Compile(input)
	if err != nil {
		b.Fatal(err)
	}
	if rules.Len() != benchmarkRuleCount {
		b.Fatalf("compiled %d rules, want %d", rules.Len(), benchmarkRuleCount)
	}
	return rules
}

type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func newBenchmarkResponseWriter() *benchmarkResponseWriter {
	return &benchmarkResponseWriter{header: make(http.Header)}
}

func (w *benchmarkResponseWriter) Header() http.Header {
	return w.header
}

func (w *benchmarkResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}

func (w *benchmarkResponseWriter) WriteHeader(status int) {
	w.status = status
}

func runAlertHandlerBenchmark(b *testing.B, handler http.Handler, payload []byte) {
	b.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts", nil)
	w := newBenchmarkResponseWriter()
	body := bytes.NewReader(nil)
	rc := io.NopCloser(body)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		body.Reset(payload)
		req.Body = rc
		w.status = 0
		handler.ServeHTTP(w, req)
		if w.status != http.StatusNoContent {
			b.Fatalf("status = %d", w.status)
		}
	}
}

// BenchmarkAlertHandler measures JSON decoding and matching for batches of
// 50-label alerts against 1000 rules. The decode-only cases make the matching
// cost visible by subtraction; rule compilation and HTTP test fixtures stay
// outside the timed loop. The allocation delta also shows that applying
// precompiled writes does not allocate per added label; allocations only
// appear when an input map grows.
func BenchmarkAlertHandler(b *testing.B) {
	rules := benchmarkAlertRules(b)
	emptyRules, err := labelmatch.Compile(nil)
	if err != nil {
		b.Fatal(err)
	}

	for _, size := range []int{1, 10, 100} {
		payload := benchmarkAlertPayload(b, size)
		b.Run(fmt.Sprintf("alerts=%d", size), func(b *testing.B) {
			b.Run("decode-only", func(b *testing.B) {
				handler := newAlertHandler(emptyRules, func([]prometheusAlert) {})
				runAlertHandlerBenchmark(b, handler, payload)
			})
			b.Run("decode-and-match", func(b *testing.B) {
				handler := newAlertHandler(rules, func([]prometheusAlert) {})
				runAlertHandlerBenchmark(b, handler, payload)
			})
		})
	}
}

func runAlertHandlerParallelBenchmark(b *testing.B, handler http.Handler, payload []byte) {
	b.Helper()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts", nil)
		w := newBenchmarkResponseWriter()
		body := bytes.NewReader(nil)
		rc := io.NopCloser(body)
		for pb.Next() {
			body.Reset(payload)
			req.Body = rc
			w.status = 0
			handler.ServeHTTP(w, req)
			if w.status != http.StatusNoContent {
				b.Errorf("status = %d", w.status)
			}
		}
	})
}

// BenchmarkAlertHandlerParallel provides the same decode-only baseline while
// exercising one RuleSet from concurrent requests. Parallelism is at the
// request level: goroutines call ServeHTTP concurrently, while each handler
// processes the alerts within its request sequentially. Each goroutine reuses
// its HTTP fixtures, and each decoder owns the maps it passes to Apply.
func BenchmarkAlertHandlerParallel(b *testing.B) {
	rules := benchmarkAlertRules(b)
	emptyRules, err := labelmatch.Compile(nil)
	if err != nil {
		b.Fatal(err)
	}
	payload := benchmarkAlertPayload(b, 100)

	b.Run("decode-only", func(b *testing.B) {
		handler := newAlertHandler(emptyRules, func([]prometheusAlert) {})
		runAlertHandlerParallelBenchmark(b, handler, payload)
	})
	b.Run("decode-and-match", func(b *testing.B) {
		handler := newAlertHandler(rules, func([]prometheusAlert) {})
		runAlertHandlerParallelBenchmark(b, handler, payload)
	})
}
