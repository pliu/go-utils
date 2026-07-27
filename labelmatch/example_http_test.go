package labelmatch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pliu/go-utils/labelmatch"
)

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

func benchmarkAlertRules(b *testing.B) *labelmatch.RuleSet {
	b.Helper()
	rules, err := labelmatch.Compile([]labelmatch.Rule{
		{
			Matchers: []labelmatch.Matcher{
				{Name: "env", Op: labelmatch.OpEqual, Value: "prod"},
				{Name: "severity", Op: labelmatch.OpRegex, Value: "critical|warning"},
			},
			Write: map[string]string{"team": "sre", "page": "yes"},
		},
		{
			Matchers: []labelmatch.Matcher{
				{Name: "env", Op: labelmatch.OpEqual, Value: "staging"},
				{Name: "severity", Op: labelmatch.OpNotEqual, Value: "critical"},
			},
			Write: map[string]string{"team": "triage"},
		},
		{
			Matchers: []labelmatch.Matcher{
				{Name: "service", Op: labelmatch.OpEqual, Value: "service-10"},
			},
			Write: map[string]string{"owner": "payments"},
		},
		{
			Matchers: []labelmatch.Matcher{
				{Name: "alertname", Op: labelmatch.OpRegex, Value: `Alert[0-9]+`},
			},
			Write: map[string]string{"source": "prometheus"},
		},
	})
	if err != nil {
		b.Fatal(err)
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

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		req.Body = io.NopCloser(bytes.NewReader(payload))
		w.status = 0
		handler.ServeHTTP(w, req)
		if w.status != http.StatusNoContent {
			b.Fatalf("status = %d", w.status)
		}
	}
}

// BenchmarkAlertHandler measures JSON decoding and matching for typical batch
// sizes. The decode-only cases make the matching cost visible by subtraction;
// rule compilation and HTTP test fixtures stay outside the timed loop.
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

// BenchmarkAlertHandlerParallel exercises the same RuleSet from concurrent
// requests; each goroutine reuses its request and response writer, while each
// decoder owns the maps that its request passes to Apply.
func BenchmarkAlertHandlerParallel(b *testing.B) {
	handler := newAlertHandler(benchmarkAlertRules(b), func([]prometheusAlert) {})
	payload := benchmarkAlertPayload(b, 100)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/alerts", nil)
		w := newBenchmarkResponseWriter()
		for pb.Next() {
			req.Body = io.NopCloser(bytes.NewReader(payload))
			w.status = 0
			handler.ServeHTTP(w, req)
			if w.status != http.StatusNoContent {
				b.Errorf("status = %d", w.status)
			}
		}
	})
}
