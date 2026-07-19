# Design: `utils` Go module — ConfigManager

## Overview

### Problem

Services need configuration that can change at runtime without a restart. Doing this correctly is fiddly and gets re-implemented (often subtly wrong) in every project:

- The config file must be validated before it is trusted; a bad edit must never take down a running service.
- Reloads must be atomic — readers should never observe a half-applied config.
- Readers must get a stable snapshot: if a reload happens after a caller fetched the config, the caller's copy must not mutate under them.

### Proposed solution

A reusable Go module, `utils`, importable by other projects. Its first package is `configmanager`, a generic, type-safe hot-reloading config loader:

- `configmanager.New[T](path, opts...)` reads the JSON file at `path`, deserializes it into a user-defined struct `T`, validates it, and fails fast if the initial load is invalid.
- The manager keeps two instances of `T`:
  - **serving** — the validated config currently in effect; what callers receive.
  - **shadow** — the staging slot. Each time the file changes, the new contents are deserialized and validated here first.
- A background goroutine watches the file. On change, the file is decoded into shadow and validated. Only if validation passes is shadow atomically promoted to serving. On failure the previous serving config stays in effect and the error is surfaced through an observability hook.
- Two read calls let the caller pick the trade-off (decided in PR review):
  - `Get()` returns a shared, read-only snapshot (`*T`) — no copy cost. Reloads swap in a *new* instance rather than mutating the served one, so a held snapshot never changes under the caller; it must simply not be mutated.
  - `GetDeepCopy()` returns a fully isolated **deep copy** (`T`) that is safe to mutate.

Validation semantics, per the requirements:

- **Unknown fields in the file are tolerated** (the struct simply doesn't map them).
- **Type mismatches on defined fields fail** (e.g. a string in the file where the struct declares an int).
- **Missing required fields fail** (see "Validation" below for how "required" is declared).
- **Duplicate JSON keys fail** (decided in PR review): `encoding/json` silently lets the last duplicate win, which hides what the file's author intended, so a token-stream scan rejects any object defining the same key twice.

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────┐
│ Manager[T]                                              │
│                                                         │
│  ┌───────────┐   load/decode/validate    ┌──────────┐   │
│  │  Poller    │ ─────────────────────▶   │  shadow  │   │
│  │ (goroutine)│                          └────┬─────┘   │
│  └───────────┘        validation OK: atomic   │         │
│        │                    swap              ▼         │
│        │ mtime/size/hash            ┌──────────────┐    │
│        ▼                            │   serving    │    │
│   config.json                       │(atomic.Pointer)   │
│                                     └──────┬───────┘    │
│               Get() ── shared snapshot ────┤            │
│        GetDeepCopy() ── deep copy ─────────┘──▶ caller  │
└─────────────────────────────────────────────────────────┘
```

**1. Loader (`load.go`)** — pure function: read file bytes → decode JSON into a fresh `T` → validate. Used identically by the initial load and by every reload, so the two paths cannot drift. Decoding uses `encoding/json` with `json.Decoder`, which by default ignores unknown fields (required behavior) and errors on type mismatches (required behavior). After decoding, a token-stream walk over the same bytes rejects duplicate keys at any nesting level (including inside unknown fields — the file is ambiguous either way), and trailing data after the top-level value is rejected via `dec.More()`.

**2. Validation (`load.go`)** — two layers, both optional but at least the first is recommended:
   - **Struct-tag validation** for "required fields": fields tagged `` `validate:"required"` `` must be present/non-zero. Implemented with `github.com/go-playground/validator/v10`, the de-facto standard, rather than hand-rolled reflection.
   - **Custom validation hook**: if `T` implements `interface{ Validate() error }`, it is called after decoding; a `WithValidator(func(*T) error)` option is also accepted for structs the user can't add methods to. This covers cross-field rules tags can't express.

**3. Store (`manager.go`)** — the serving instance is held in an `atomic.Pointer[T]`. Promotion of shadow → serving is a single pointer store: readers never see a torn config and readers never block writers (no `RWMutex` contention on the hot path). Once promoted, the instance is treated as immutable — the next reload decodes into a brand-new `T`, never into the currently-serving one.

**4. Poller (`watch.go`)** — a background goroutine polls the file on a configurable interval (default 3s). A cheap `os.Stat` check (mtime + size) gates a content read; a SHA-256 of the contents is compared against the last-loaded hash so touched-but-unchanged files and mtime-granularity issues don't cause spurious reloads. Rationale for polling over fsnotify:
   - **Zero dependencies and no platform-specific edge cases.** fsnotify famously misbehaves with atomic-rename writes (`vim`, `sed -i`), symlink swaps (Kubernetes ConfigMaps), and editors that replace inodes — exactly how config files are edited in practice. Handling those correctly with fsnotify ends up re-implementing polling as a fallback anyway.
   - Config files are small and change rarely; a stat every few seconds is negligible.
   - The poller interface is internal, so an fsnotify-based watcher can be added later behind an option without breaking the API.

**5. Read path — `Get()` and `GetDeepCopy()`** — two calls so the caller chooses the trade-off (per PR review):
   - `Get() *T` — an atomic pointer load, no copying. The returned snapshot is shared and read-only: callers must not mutate it. It is nonetheless stable — a reload swaps the serving pointer to a brand-new instance and never mutates a published one, so a held snapshot keeps its values forever.
   - `GetDeepCopy() T` — a fully isolated copy that is safe to mutate. A shallow struct copy would not be enough: slices, maps, and pointers inside `T` would share backing memory with other callers. The deep copy is implemented by JSON round-trip (`json.Marshal` the serving instance → `json.Unmarshal` into a fresh `T`): guaranteed correct for any type that round-trips through JSON — which `T` must, since it's loaded from JSON — with no reflection code to maintain.

**6. Lifecycle & observability (`manager.go`, `options.go`)**
   - `New` fails (returns an error, no manager) if the initial load or validation fails — a service never starts on a bad config.
   - `Close()` stops the poller goroutine (internally: context cancellation).
   - `WithOnSwap(func(old, new *T))` — called after each successful promotion (logging, metric bumps, re-deriving cached values); the arguments are shared snapshots with the same must-not-mutate contract as `Get`.
   - `WithOnError(func(error))` — called when a reload fails (unreadable file, bad JSON, validation failure). The failure never affects serving; this hook exists so failures are visible instead of silent.
   - `Err()` returns the most recent reload error (nil if the last reload succeeded), for health checks.

### Data flow

**Initial load (synchronous, in `New`):**
1. Read file at `path`. Missing/unreadable file → error, `New` fails.
2. Decode JSON into fresh `T`. Syntax error, type mismatch, duplicate key, or trailing data → error, `New` fails. Unknown fields ignored.
3. Validate (tags, then custom hook). Failure → error, `New` fails.
4. Store instance in serving pointer; record content hash; start poller.

**Reload (background):**
1. Poller tick → `os.Stat`; if mtime/size unchanged, done.
2. Read contents; if SHA-256 equals last-loaded hash, done.
3. Decode into a fresh shadow `T` and validate — same code path as steps 2–3 above.
4. On failure: serving untouched, record error, fire `OnError`, retry on next tick.
5. On success: atomically swap shadow into serving, update hash, clear `Err()`, fire `OnSwap`.

**Read:**
1. `Get()` → atomic load of the serving pointer → returned as a shared read-only snapshot.
2. `GetDeepCopy()` → atomic load → JSON round-trip deep copy → returned by value.

### Sketch of the public API

```go
type Manager[T any] struct { /* unexported */ }

func New[T any](path string, opts ...Option[T]) (*Manager[T], error)

func (m *Manager[T]) Get() *T        // shared read-only snapshot of the serving config
func (m *Manager[T]) GetDeepCopy() T // isolated deep copy, safe to mutate
func (m *Manager[T]) Err() error     // most recent reload error, nil if healthy
func (m *Manager[T]) Close()         // stop watching

func WithPollInterval[T any](d time.Duration) Option[T]
func WithValidator[T any](fn func(*T) error) Option[T]
func WithOnSwap[T any](fn func(old, new *T)) Option[T]
func WithOnError[T any](fn func(error)) Option[T]
```

### Key technology choices

| Choice | Rationale |
|---|---|
| Go generics (`Manager[T]`) | Compile-time type safety; no `interface{}` casts at call sites. Requires Go ≥ 1.18; we target a recent stable Go (1.22+). |
| `encoding/json` (stdlib) | Default behavior matches the spec: unknown fields ignored, type mismatches error. A small token-stream scan adds duplicate-key rejection on top. No dependency. |
| `go-playground/validator` | Declarative `required` (and richer) rules via struct tags; the community standard. The only third-party dependency. |
| `atomic.Pointer[T]` for serving | Lock-free reads, atomic swap; simplest correct primitive for read-mostly data. |
| Polling + stat gate + content hash | Robust against atomic renames/symlink swaps where fsnotify is not; zero deps; trivial to reason about. |
| `Get()`/`GetDeepCopy()` split | Caller picks: zero-cost shared snapshot vs. isolated mutable copy (JSON round-trip — guaranteed correct for JSON-loaded types; no bespoke reflection copier to maintain). |

## Proposed project layout

The module is `utils`, with each tool in its own package so importers only pull what they use. ConfigManager lives in `configmanager`; future tools get sibling directories.

```
utils/
├── go.mod                        # module github.com/pliu/utils
├── README.md                     # module overview + per-tool index
├── DESIGN.md                     # this document
└── configmanager/
    ├── configmanager.go          # Manager[T], New, Get, Err, Close
    ├── options.go                # Option[T] and With* constructors
    ├── load.go                   # read + decode + validate (shared by init & reload)
    ├── watch.go                  # poller goroutine, stat/hash change detection
    ├── configmanager_test.go     # unit + integration tests
    ├── example_test.go           # godoc-rendered usage example
    └── testdata/
        ├── valid.json
        ├── missing_required.json
        └── type_mismatch.json
```

## Milestones

Each milestone is a small, independently reviewable PR that leaves the module in a working state.

1. **M1 — Module scaffolding + synchronous core.** `go.mod`, CI (build, `go vet`, `go test`, lint). Implement `load.go` and `New`/`Get`/`GetDeepCopy`/`Close` with no watching yet: initial load, decode (incl. duplicate-key rejection), tag + custom validation. Tests: valid file, missing file, malformed JSON, unknown fields tolerated, type mismatch rejected, missing required field rejected, duplicate keys rejected, `GetDeepCopy` isolation (mutating the returned value and its nested slices/maps doesn't affect subsequent calls or the serving config).
2. **M2 — Hot reload.** Poller with stat gate + content hash, shadow decode/validate, atomic promotion, `Err()`. Tests: file change picked up, invalid change keeps old config serving and sets `Err()`, recovery after a bad write, atomic-rename and symlink-swap writes detected, no reload when only mtime changes, concurrent `Get` during swaps under `-race`.
3. **M3 — Options & observability.** `WithPollInterval`, `WithValidator`, `WithOnSwap`, `WithOnError`; callback ordering/panic-safety tests.
4. **M4 — Docs & release.** README with quick-start, `example_test.go`, full godoc comments, tag `v0.1.0`.

## Clarifying questions

Answers to these would most change the design. Each has an assumed answer so implementation can proceed if unanswered.

1. **How should "required fields" be declared?** The spec says validation must fail on missing required fields, but plain Go structs don't express requiredness — a missing JSON field just leaves the zero value.
   *Assumed:* fields tagged `` `validate:"required"` `` (go-playground/validator), plus an optional custom validation hook for cross-field rules. If a zero-dependency module is preferred, we'd instead rely solely on the custom hook and implement a small reflection-based `required` tag check ourselves.

2. ~~**Is per-call deep-copy cost acceptable for `Get()`?**~~ **Answered in PR review:** expose both — `Get()` returns a shared read-only snapshot with no copy cost, and `GetDeepCopy()` returns an isolated deep copy — so the user chooses per call site.

3. **Polling vs. OS file notifications, and what interval?**
   *Assumed:* polling, default 3s, configurable via `WithPollInterval`. fsnotify support can be added later behind an option if sub-second reaction time is ever needed.

4. **Should `shadow` be observable to users** (e.g. an API to inspect the last rejected candidate config for debugging), or is it purely an internal staging slot?
   *Assumed:* purely internal; `Err()` and `WithOnError` expose *why* a candidate was rejected, which covers the debugging need without widening the API.

5. **What happens if the file is deleted (or briefly missing mid-rename) after startup?**
   *Assumed:* treated like any other failed reload — keep serving the last good config, surface via `Err()`/`WithOnError`, keep polling so the config heals when the file reappears.

6. **Module path and Go version?**
   *Assumed:* `module github.com/pliu/utils`, Go 1.22.

7. ~~**Strictness beyond the spec:** should a *duplicate* key or trailing garbage in the JSON be rejected?~~ **Answered in PR review:** reject duplicate keys — they make it difficult to determine what the author actually intended. Trailing garbage is rejected as well (cheap `dec.More()` check, almost always indicates a corrupt write).
