package configmanager

import "github.com/prometheus/client_golang/prometheus"

type prometheusMetrics struct {
	loadDuration         prometheus.Histogram
	lastReloadSuccessful prometheus.Gauge
}

func newPrometheusMetrics() *prometheusMetrics {
	return &prometheusMetrics{
		loadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "configmanager",
			Name:      "load_duration_seconds",
			Help:      "Duration in seconds of successful config file loads, including the initial load and reloads.",
		}),
		lastReloadSuccessful: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "configmanager",
			Name:      "last_reload_successful",
			Help:      "Whether the most recent config load or reload attempt succeeded (1 for success, 0 for failure).",
		}),
	}
}

func (m *prometheusMetrics) Describe(ch chan<- *prometheus.Desc) {
	m.loadDuration.Describe(ch)
	m.lastReloadSuccessful.Describe(ch)
}

func (m *prometheusMetrics) Collect(ch chan<- prometheus.Metric) {
	m.loadDuration.Collect(ch)
	m.lastReloadSuccessful.Collect(ch)
}

// PrometheusCollector returns this Manager's Prometheus metrics collector.
// The collector is not registered automatically; callers can register it
// with their own prometheus.Registerer and expose that registry however they
// choose. Repeated calls return the same collector.
//
// Registering collectors from multiple Managers in one registry requires
// distinguishing them with const labels, for example by using
// prometheus.WrapRegistererWith.
func (m *Manager[T]) PrometheusCollector() prometheus.Collector {
	return m.metrics
}
