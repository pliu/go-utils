package labelmatch_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/pliu/go-utils/configmanager"
	"github.com/pliu/go-utils/labelmatch"
)

// ruleConfig is a config file holding a rule set. Compiling inside Validate,
// which configmanager runs on the decoded instance before promoting it,
// stashes the compiled RuleSet on that same instance — so the manager's own
// atomic swap carries the compiled rules, with no extra wiring and no second
// compile.
type ruleConfig struct {
	Rules    []labelmatch.Rule `json:"rules"`
	compiled *labelmatch.RuleSet
}

// Validate compiles the rules and keeps the result. Returning an error here
// rejects the file before it is promoted, so rules that do not compile never
// take effect and the previously compiled rules keep serving.
func (c *ruleConfig) Validate() error {
	rs, err := labelmatch.Compile(c.Rules)
	if err != nil {
		return err
	}
	c.compiled = rs
	return nil
}

// RuleSet returns the rules compiled during validation. Read it from
// Manager.Get, which returns the validated instance; Manager.GetDeepCopy
// round-trips through JSON and cannot carry an unexported field.
func (c *ruleConfig) RuleSet() *labelmatch.RuleSet {
	if c.compiled == nil {
		panic("ruleConfig was not validated (GetDeepCopy?)")
	}
	return c.compiled
}

// Rules can live in a config file and be recompiled whenever it changes.
func Example_hotReload() {
	dir, err := os.MkdirTemp("", "labelmatch-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rules.json")

	writeRules := func(team string) {
		rules := fmt.Sprintf(
			`{"rules":[{"matchers":[{"name":"env","op":"=","value":"prod"}],"write":{"team":%q}}]}`,
			team)
		if err := os.WriteFile(path, []byte(rules), 0o600); err != nil {
			log.Fatal(err)
		}
	}
	writeRules("platform")

	mgr, err := configmanager.New[ruleConfig](path,
		configmanager.WithPollInterval[ruleConfig](10*time.Millisecond))
	if err != nil {
		log.Fatal(err)
	}
	defer mgr.Close()

	// The compiled rules ride along with the config, so there is nothing to
	// seed and no window where the rule set is missing.
	team := func() string {
		labels := map[string]string{"env": "prod"}
		mgr.Get().RuleSet().Apply(labels)
		return labels["team"]
	}
	fmt.Println("initial:", team())

	// A changed file is recompiled and swapped in atomically.
	writeRules("sre")
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && team() == "platform"; {
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Println("after reload:", team())

	// A file whose rules do not compile is rejected by Validate before
	// promotion, so the rules above stay in effect.
	if err := os.WriteFile(path, []byte(
		`{"rules":[{"matchers":[{"name":"env","op":"??","value":"prod"}],"write":{"team":"broken"}}]}`,
	), 0o600); err != nil {
		log.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && mgr.Err() == nil; {
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Println("after bad rules:", team())
	fmt.Println("error reported:", mgr.Err() != nil)

	// Output:
	// initial: platform
	// after reload: sre
	// after bad rules: sre
	// error reported: true
}
