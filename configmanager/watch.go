package configmanager

import (
	"context"
	"fmt"
	"os"
	"time"
)

func (m *Manager[T]) watch(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.opts.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

// poll checks the file for changes and, if it changed, decodes and validates
// it into a fresh shadow instance, promoting it to serving on success. It
// runs only on the poller goroutine, which exclusively owns lastHash,
// lastMod, and lastSize after New returns.
func (m *Manager[T]) poll() {
	fi, err := os.Stat(m.path)
	if err != nil {
		m.setErr(fmt.Errorf("configmanager: stat %s: %w", m.path, err))
		return
	}
	// Cheap gate: skip reading the file if it hasn't visibly changed since
	// the last poll. Record what we saw so a persistently bad file is
	// re-processed only when it changes again, not on every tick.
	if fi.ModTime().Equal(m.lastMod) && fi.Size() == m.lastSize {
		return
	}
	m.lastMod = fi.ModTime()
	m.lastSize = fi.Size()

	res, err := load(m.path, m.opts.validator)
	if err != nil {
		m.setErr(fmt.Errorf("configmanager: reload of %s: %w", m.path, err))
		return
	}
	if res.hash == m.lastHash {
		// Content identical to the last good load (e.g. touched, or reverted
		// after a bad edit): nothing to swap, and the file is healthy again.
		m.setErr(nil)
		return
	}
	old := m.serving.Swap(res.cfg)
	m.lastHash = res.hash
	m.setErr(nil)
	if m.opts.onSwap != nil {
		m.opts.onSwap(old, res.cfg)
	}
}
