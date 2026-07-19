package configmanager

import (
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type gatheredMetrics struct {
	loadCount            uint64
	lastReloadSuccessful float64
}

func gatherMetrics(t *testing.T, registry *prometheus.Registry) gatheredMetrics {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	var got gatheredMetrics
	found := make(map[string]bool, 2)
	for _, family := range families {
		if len(family.Metric) == 0 {
			continue
		}
		metric := family.Metric[0]
		switch family.GetName() {
		case "configmanager_load_duration_seconds":
			got.loadCount = metric.GetHistogram().GetSampleCount()
			found[family.GetName()] = true
		case "configmanager_last_reload_successful":
			got.lastReloadSuccessful = metric.GetGauge().GetValue()
			found[family.GetName()] = true
		}
	}
	if len(found) != 2 {
		t.Fatalf("gathered %d of 2 configmanager metrics: %v", len(found), found)
	}
	return got
}

func TestPrometheusMetrics(t *testing.T) {
	m, path := newTestManager(t, validJSON)
	collector := m.PrometheusCollector()
	if collector != m.PrometheusCollector() {
		t.Fatal("PrometheusCollector returned a different collector")
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	if got := gatherMetrics(t, registry); got.loadCount != 1 || got.lastReloadSuccessful != 1 {
		t.Fatalf("metrics after initial load = %+v", got)
	}

	writeFile(t, path, `{"name": "bad"}`)
	eventually(t, func() bool {
		got := gatherMetrics(t, registry)
		return got.loadCount == 1 && got.lastReloadSuccessful == 0
	}, "failed reload recorded in metrics")

	// Reverting to the already-serving content is still a successful load.
	writeFile(t, path, validJSON)
	eventually(t, func() bool {
		got := gatherMetrics(t, registry)
		return got.loadCount == 2 && got.lastReloadSuccessful == 1
	}, "unchanged successful load recorded in metrics")

	writeFile(t, path, `{"name": "new", "port": 9000}`)
	eventually(t, func() bool {
		got := gatherMetrics(t, registry)
		return got.loadCount == 3 && got.lastReloadSuccessful == 1
	}, "successful reload recorded in metrics")

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return gatherMetrics(t, registry).lastReloadSuccessful == 0
	}, "stat failure recorded in metrics")
	if got := gatherMetrics(t, registry); got.loadCount != 3 {
		t.Fatalf("metrics after stat failure = %+v", got)
	}
}
