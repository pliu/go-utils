package configmanager

import "time"

// DefaultPollInterval is how often the file is checked for changes unless
// overridden with WithPollInterval.
const DefaultPollInterval = 3 * time.Second

type options[T any] struct {
	pollInterval time.Duration
	validator    func(*T) error
	onSwap       func(old, new *T)
	onError      func(error)
}

func defaultOptions[T any]() options[T] {
	return options[T]{pollInterval: DefaultPollInterval}
}

// Option configures a Manager.
type Option[T any] func(*options[T])

// WithPollInterval sets how often the config file is polled for changes.
// Values <= 0 are ignored.
func WithPollInterval[T any](d time.Duration) Option[T] {
	return func(o *options[T]) {
		if d > 0 {
			o.pollInterval = d
		}
	}
}

// WithValidator adds a custom validation function, called after decoding and
// struct-tag validation. It is useful for cross-field rules that tags cannot
// express, or for types the user cannot add a Validate method to. Returning
// a non-nil error rejects the config.
func WithValidator[T any](fn func(*T) error) Option[T] {
	return func(o *options[T]) { o.validator = fn }
}

// WithOnSwap registers a callback invoked after each successful hot reload,
// with the previous and newly serving configs. Both are shared, read-only
// snapshots (see Get) and must not be mutated. The callback runs on the
// poller goroutine; a slow callback delays subsequent polls, and calling
// Close from it deadlocks.
func WithOnSwap[T any](fn func(old, new *T)) Option[T] {
	return func(o *options[T]) { o.onSwap = fn }
}

// WithOnError registers a callback invoked whenever a reload attempt fails
// (unreadable file, bad JSON, validation failure). The previously serving
// config remains in effect. The callback runs on the poller goroutine;
// calling Close from it deadlocks.
//
// A file whose content decodes or validates badly fires the callback once
// per change; a file that cannot be stat'ed (e.g. deleted) fires it on
// every poll tick until the file is readable again.
func WithOnError[T any](fn func(error)) Option[T] {
	return func(o *options[T]) { o.onError = fn }
}
