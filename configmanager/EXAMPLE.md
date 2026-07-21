# `configmanager` example

## Loading and reading configuration

```go
import "github.com/pliu/go-utils/configmanager"

type AppConfig struct {
    ListenAddr string `json:"listen_addr" validate:"required"`
    MaxConns   int    `json:"max_conns"`
}

mgr, err := configmanager.New[AppConfig]("/etc/app/config.json")
if err != nil {
    log.Fatal(err) // invalid initial config: fail fast, never start on a bad config
}
defer mgr.Close()

cfg := mgr.Get()          // shared read-only snapshot (do not mutate)
mine := mgr.GetDeepCopy() // fully isolated copy (safe to mutate)
```

## Prometheus metrics

Each manager's collector can be registered with an application's own
Prometheus registry and served through its metrics handler:

```go
registry := prometheus.NewRegistry()
registry.MustRegister(mgr.PrometheusCollector())

metricsHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
```

Collectors from multiple managers need const labels to distinguish them:

```go
registerer := prometheus.WrapRegistererWith(
    prometheus.Labels{"config": "application"},
    registry,
)
registerer.MustRegister(mgr.PrometheusCollector())
```
