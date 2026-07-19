package configmanager

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/go-playground/validator/v10"
)

// tagValidator is safe for concurrent use and caches struct metadata, so a
// single shared instance is used for all Manager instantiations.
var tagValidator = validator.New(validator.WithRequiredStructEnabled())

// Validatable can be implemented by config types to run custom validation
// after decoding and struct-tag validation.
type Validatable interface {
	Validate() error
}

type loadResult[T any] struct {
	cfg     *T
	hash    [sha256.Size]byte
	modTime time.Time
	size    int64
}

// load is the single code path shared by the initial load in New and every
// background reload: read the file, decode it into a fresh T, and validate.
func load[T any](path string, custom func(*T) error) (loadResult[T], error) {
	var res loadResult[T]
	// Stat before the read: if the file changes between the two, the recorded
	// mtime/size are stale in the safe direction — the poller's next stat
	// differs and triggers a re-read. The reverse order could record the new
	// file's stat against the old file's content, suppressing the re-read.
	if fi, err := os.Stat(path); err == nil {
		res.modTime = fi.ModTime()
		res.size = fi.Size()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return res, err
	}

	cfg := new(T)
	if err := decode(data, cfg); err != nil {
		return res, err
	}
	if err := validate(cfg, custom); err != nil {
		return res, fmt.Errorf("validation: %w", err)
	}
	res.cfg = cfg
	res.hash = sha256.Sum256(data)
	return res, nil
}

// decode unmarshals data into v with the semantics required of the config
// file: unknown fields are tolerated, type mismatches on defined fields are
// errors, duplicate keys are errors, and trailing data after the top-level
// value is an error.
func decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return err
	}
	// Runs after Decode so syntax errors surface with the standard
	// encoding/json messages; by this point the data is known-valid JSON.
	walk := json.NewDecoder(bytes.NewReader(data))
	if err := checkDupValue(walk); err != nil {
		return err
	}
	// checkDupValue consumed exactly one top-level value, so anything left is
	// trailing data. (dec.More cannot detect this: it reports false for a
	// trailing '}' or ']'.)
	if _, err := walk.Token(); err != io.EOF {
		return errors.New("trailing data after top-level JSON value")
	}
	return nil
}

func validate[T any](cfg *T, custom func(*T) error) error {
	// validator.Struct only accepts structs; skip tag validation for other
	// top-level types (e.g. a map or slice config).
	if reflect.ValueOf(cfg).Elem().Kind() == reflect.Struct {
		if err := tagValidator.Struct(cfg); err != nil {
			return err
		}
	}
	if v, ok := any(cfg).(Validatable); ok {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	if custom != nil {
		if err := custom(cfg); err != nil {
			return err
		}
	}
	return nil
}

// checkDupValue consumes one JSON value from the token stream and rejects
// objects that define the same key more than once at the same nesting level —
// including inside objects the target struct does not map. encoding/json
// silently lets the last duplicate win, which hides what the author of the
// file actually intended.
func checkDupValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil // scalar
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key := keyTok.(string)
			if _, dup := seen[key]; dup {
				return fmt.Errorf("duplicate key %q in JSON object", key)
			}
			seen[key] = struct{}{}
			if err := checkDupValue(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return err
		}
	case '[':
		for dec.More() {
			if err := checkDupValue(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return err
		}
	}
	return nil
}
