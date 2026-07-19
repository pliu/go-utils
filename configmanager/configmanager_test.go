package configmanager

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testConfig struct {
	Name     string            `json:"name" validate:"required"`
	Port     int               `json:"port" validate:"required"`
	Tags     []string          `json:"tags"`
	Extra    map[string]string `json:"extra"`
	Nested   nested            `json:"nested"`
	Optional string            `json:"optional"`
}

type nested struct {
	Value int `json:"value"`
}

const validJSON = `{
	"name": "svc",
	"port": 8080,
	"tags": ["a", "b"],
	"extra": {"k": "v"},
	"nested": {"value": 7}
}`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestManager(t *testing.T, content string, opts ...Option[testConfig]) (*Manager[testConfig], string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, content)
	opts = append([]Option[testConfig]{WithPollInterval[testConfig](5 * time.Millisecond)}, opts...)
	m, err := New(path, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)
	return m, path
}

// eventually polls fn until it returns true or the deadline passes.
func eventually(t *testing.T, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within deadline: %s", msg)
}

func TestInitialLoad(t *testing.T) {
	m, _ := newTestManager(t, validJSON)
	got := m.Get()
	if got.Name != "svc" || got.Port != 8080 || got.Nested.Value != 7 {
		t.Errorf("unexpected config: %+v", got)
	}
	if m.Err() != nil {
		t.Errorf("Err() = %v, want nil", m.Err())
	}
}

func TestInitialLoadFailures(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantSub string // any of the |-separated substrings must appear
	}{
		{"malformed JSON", `{"name": "svc",`, "unexpected"},
		{"type mismatch", `{"name": "svc", "port": "not-a-number"}`, "cannot unmarshal"},
		{"missing required field", `{"name": "svc"}`, "required"},
		{"duplicate key", `{"name": "svc", "port": 1, "name": "other"}`, "duplicate key"},
		{"duplicate key in nested object", `{"name": "svc", "port": 1, "nested": {"value": 1, "value": 2}}`, "duplicate key"},
		{"trailing data", `{"name": "svc", "port": 1} {"more": true}`, "trailing data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tc.content)
			_, err := New[testConfig](path)
			if err == nil {
				t.Fatalf("New succeeded, want error containing %q", tc.wantSub)
			}
			matched := false
			for sub := range strings.SplitSeq(tc.wantSub, "|") {
				if strings.Contains(err.Error(), sub) {
					matched = true
				}
			}
			if !matched {
				t.Errorf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

func TestMissingFile(t *testing.T) {
	_, err := New[testConfig](filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("New succeeded for a missing file")
	}
}

func TestUnknownFieldsTolerated(t *testing.T) {
	m, _ := newTestManager(t, `{"name": "svc", "port": 1, "not_in_struct": {"deep": [1, 2]}}`)
	if got := m.Get(); got.Name != "svc" || got.Port != 1 {
		t.Errorf("unexpected config: %+v", got)
	}
}

func TestDuplicateKeysRejectedInUnknownFields(t *testing.T) {
	// Duplicate keys are rejected even inside objects the struct doesn't map:
	// the file is ambiguous regardless of whether we read the field today.
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"name": "svc", "port": 1, "unknown": {"a": 1, "a": 2}}`)
	if _, err := New[testConfig](path); err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("New err = %v, want duplicate key error", err)
	}
}

func TestGetReturnsSharedSnapshot(t *testing.T) {
	m, _ := newTestManager(t, validJSON)
	a, b := m.Get(), m.Get()
	if a != b {
		t.Error("Get returned different pointers with no reload in between")
	}
}

func TestGetDeepCopyIsolation(t *testing.T) {
	m, _ := newTestManager(t, validJSON)
	c1 := m.GetDeepCopy()
	c1.Name = "mutated"
	c1.Tags[0] = "mutated"
	c1.Extra["k"] = "mutated"
	c1.Nested.Value = -1
	if c1.Name != "mutated" || c1.Nested.Value != -1 {
		t.Fatalf("copy did not accept mutation: %+v", c1)
	}

	c2 := m.GetDeepCopy()
	if c2.Name != "svc" || c2.Tags[0] != "a" || c2.Extra["k"] != "v" || c2.Nested.Value != 7 {
		t.Errorf("mutation of one deep copy leaked into another: %+v", c2)
	}
	served := m.Get()
	if served.Tags[0] != "a" || served.Extra["k"] != "v" {
		t.Errorf("mutation of a deep copy leaked into the serving config: %+v", served)
	}
}

func TestHotReload(t *testing.T) {
	m, path := newTestManager(t, validJSON)
	before := m.Get()
	writeFile(t, path, `{"name": "svc2", "port": 9090}`)
	eventually(t, func() bool { return m.Get().Name == "svc2" }, "reload picked up")
	if m.Get().Port != 9090 {
		t.Errorf("Port = %d, want 9090", m.Get().Port)
	}
	if before.Name != "svc" {
		t.Errorf("previously returned snapshot changed under the caller: %+v", before)
	}
}

func TestInvalidReloadKeepsServing(t *testing.T) {
	m, path := newTestManager(t, validJSON)
	writeFile(t, path, `{"name": "svc"}`) // missing required port
	eventually(t, func() bool { return m.Err() != nil }, "Err() set after bad reload")
	if got := m.Get(); got.Name != "svc" || got.Port != 8080 {
		t.Errorf("serving config changed after invalid reload: %+v", got)
	}

	// Recovery: a good write heals the manager.
	writeFile(t, path, `{"name": "healed", "port": 1}`)
	eventually(t, func() bool { return m.Get().Name == "healed" }, "recovered after bad write")
	eventually(t, func() bool { return m.Err() == nil }, "Err() cleared after recovery")
}

func TestFileDeletedKeepsServing(t *testing.T) {
	m, path := newTestManager(t, validJSON)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return m.Err() != nil }, "Err() set after file deleted")
	if got := m.Get(); got.Name != "svc" {
		t.Errorf("serving config changed after file deletion: %+v", got)
	}
	// File reappears with new content.
	writeFile(t, path, `{"name": "back", "port": 2}`)
	eventually(t, func() bool { return m.Get().Name == "back" }, "reload after file reappeared")
}

func TestAtomicRenameWrite(t *testing.T) {
	m, path := newTestManager(t, validJSON)
	tmp := path + ".tmp"
	writeFile(t, tmp, `{"name": "renamed", "port": 3}`)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return m.Get().Name == "renamed" }, "atomic-rename write picked up")
}

func TestSymlinkSwap(t *testing.T) {
	dir := t.TempDir()
	v1 := filepath.Join(dir, "v1.json")
	v2 := filepath.Join(dir, "v2.json")
	link := filepath.Join(dir, "config.json")
	writeFile(t, v1, validJSON)
	writeFile(t, v2, `{"name": "v2", "port": 4}`)
	if err := os.Symlink(v1, link); err != nil {
		t.Fatal(err)
	}
	m, err := New[testConfig](link, WithPollInterval[testConfig](5*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	// Swap the symlink target atomically, as Kubernetes does for ConfigMaps.
	tmpLink := filepath.Join(dir, "config.json.tmp")
	if err := os.Symlink(v2, tmpLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpLink, link); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return m.Get().Name == "v2" }, "symlink swap picked up")
}

func TestOnSwapAndOnError(t *testing.T) {
	var swaps, errs atomic.Int64
	var gotOld, gotNew atomic.Pointer[testConfig]
	m, path := newTestManager(t, validJSON,
		WithOnSwap[testConfig](func(old, new *testConfig) {
			gotOld.Store(old)
			gotNew.Store(new)
			swaps.Add(1)
		}),
		WithOnError[testConfig](func(error) { errs.Add(1) }),
	)

	writeFile(t, path, `{"name": "bad"}`)
	eventually(t, func() bool { return errs.Load() > 0 }, "OnError fired")
	if swaps.Load() != 0 {
		t.Errorf("OnSwap fired %d times for an invalid config", swaps.Load())
	}

	writeFile(t, path, `{"name": "good", "port": 5}`)
	eventually(t, func() bool { return swaps.Load() == 1 }, "OnSwap fired")
	if gotOld.Load().Name != "svc" || gotNew.Load().Name != "good" {
		t.Errorf("OnSwap args: old=%+v new=%+v", gotOld.Load(), gotNew.Load())
	}
	if gotNew.Load() != m.Get() {
		t.Error("OnSwap new arg is not the serving instance")
	}
}

func TestCustomValidatorOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, validJSON)
	_, err := New(path, WithValidator(func(c *testConfig) error {
		if c.Port < 10000 {
			return errors.New("port must be >= 10000")
		}
		return nil
	}))
	if err == nil || !strings.Contains(err.Error(), "port must be >= 10000") {
		t.Fatalf("New err = %v, want custom validator error", err)
	}
}

type selfValidating struct {
	Threshold int `json:"threshold"`
}

func (s *selfValidating) Validate() error {
	if s.Threshold < 0 {
		return errors.New("threshold must be non-negative")
	}
	return nil
}

func TestValidatableInterface(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"threshold": -1}`)
	if _, err := New[selfValidating](path); err == nil || !strings.Contains(err.Error(), "threshold must be non-negative") {
		t.Fatalf("New err = %v, want Validate() error", err)
	}
	writeFile(t, path, `{"threshold": 1}`)
	m, err := New[selfValidating](path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.Close()
}

func TestNonStructConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"a": 1, "b": 2}`)
	m, err := New[map[string]int](path)
	if err != nil {
		t.Fatalf("New with map config: %v", err)
	}
	defer m.Close()
	if got := m.GetDeepCopy(); got["a"] != 1 || got["b"] != 2 {
		t.Errorf("unexpected map config: %v", got)
	}
}

func TestCloseIdempotent(t *testing.T) {
	m, _ := newTestManager(t, validJSON)
	m.Close()
	m.Close()
	if got := m.Get(); got.Name != "svc" {
		t.Errorf("Get after Close: %+v", got)
	}
}

func TestConcurrentGetDuringSwaps(t *testing.T) {
	m, path := newTestManager(t, validJSON)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				got := m.Get()
				if got.Name == "" || got.Port == 0 {
					t.Error("observed torn/invalid config")
					return
				}
				_ = m.GetDeepCopy()
			}
		}
	}()
	for i := range 20 {
		writeFile(t, path, `{"name": "svc", "port": `+string(rune('1'+i%9))+`}`)
		time.Sleep(3 * time.Millisecond)
	}
	close(stop)
	<-done
}
