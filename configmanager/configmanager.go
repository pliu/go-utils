// Package configmanager provides a generic, type-safe, hot-reloading JSON
// configuration loader.
//
// A Manager[T] reads a JSON file into a user-defined struct T, validates it,
// and serves it to callers. In the background it polls the file for changes;
// a changed file is deserialized and validated into a fresh shadow instance,
// which is atomically promoted to the serving instance only if validation
// passes. A failed reload never disturbs the config currently being served.
package configmanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Manager watches a JSON config file and serves validated instances of T.
// All methods are safe for concurrent use.
type Manager[T any] struct {
	path string
	opts options[T]

	serving atomic.Pointer[T]

	// Owned by the poller goroutine after New returns.
	lastHash [sha256.Size]byte
	lastMod  time.Time
	lastSize int64

	errMu   sync.Mutex
	lastErr error

	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// New reads, deserializes, and validates the JSON file at path into a T.
// It returns an error (and no Manager) if the file cannot be read, is not
// valid JSON, contains duplicate keys or trailing data, has a type mismatch
// on a defined field, or fails validation — a service never starts on a bad
// config. Unknown fields in the file are tolerated. On success it starts a
// background poller that hot-reloads the file on change; stop it with Close.
func New[T any](path string, opts ...Option[T]) (*Manager[T], error) {
	o := defaultOptions[T]()
	for _, opt := range opts {
		opt(&o)
	}
	m := &Manager[T]{path: path, opts: o}

	res, err := load(path, o.validator)
	if err != nil {
		return nil, fmt.Errorf("configmanager: initial load of %s: %w", path, err)
	}
	m.serving.Store(res.cfg)
	m.lastHash = res.hash
	m.lastMod = res.modTime
	m.lastSize = res.size

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	go m.watch(ctx)
	return m, nil
}

// Get returns the config currently being served, without copying.
//
// The returned value is a shared, read-only snapshot: callers must not
// mutate it (directly or through nested slices, maps, or pointers). The
// pointer remains valid and unchanged after a hot reload — reloads swap in
// a new instance rather than modifying this one — so a caller holding it
// keeps a stable view. Callers that need a mutable or fully isolated copy
// should use GetDeepCopy.
func (m *Manager[T]) Get() *T {
	return m.serving.Load()
}

// GetDeepCopy returns a deep copy of the config currently being served.
// The copy shares no memory with the Manager or with other callers, so it
// may be freely mutated and is unaffected by later reloads.
//
// The copy is produced by a JSON round-trip, which is guaranteed to work
// for any T loaded from JSON. It panics only if T has been made
// un-marshalable (a programmer error, e.g. a channel field), which the
// initial load would already have rejected on the decode side.
func (m *Manager[T]) GetDeepCopy() T {
	cur := m.serving.Load()
	b, err := json.Marshal(cur)
	if err != nil {
		panic(fmt.Sprintf("configmanager: config type %T does not round-trip through JSON: %v", cur, err))
	}
	out := new(T)
	if err := json.Unmarshal(b, out); err != nil {
		panic(fmt.Sprintf("configmanager: config type %T does not round-trip through JSON: %v", cur, err))
	}
	return *out
}

// Err returns the most recent reload error, or nil if the last reload
// attempt succeeded (or no reload has been attempted). A non-nil error
// means the file on disk is currently bad and the Manager is still serving
// the last good config.
func (m *Manager[T]) Err() error {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	return m.lastErr
}

// Close stops the background poller. It is idempotent and safe to call
// concurrently. Get and GetDeepCopy remain usable after Close; the config
// simply no longer reloads.
func (m *Manager[T]) Close() {
	m.closeOnce.Do(func() {
		m.cancel()
		<-m.done
	})
}

func (m *Manager[T]) setErr(err error) {
	m.errMu.Lock()
	m.lastErr = err
	m.errMu.Unlock()
	if err != nil && m.opts.onError != nil {
		m.opts.onError(err)
	}
}
