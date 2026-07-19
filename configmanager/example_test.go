package configmanager_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/pliu/utils/configmanager"
)

type AppConfig struct {
	ListenAddr string   `json:"listen_addr" validate:"required"`
	Features   []string `json:"features"`
}

func Example() {
	path := filepath.Join(os.TempDir(), "example-config.json")
	if err := os.WriteFile(path, []byte(`{"listen_addr": ":8080", "features": ["gzip"]}`), 0o644); err != nil {
		log.Fatal(err)
	}
	defer os.Remove(path)

	mgr, err := configmanager.New[AppConfig](path,
		configmanager.WithOnSwap[AppConfig](func(old, new *AppConfig) {
			log.Printf("config reloaded: %s -> %s", old.ListenAddr, new.ListenAddr)
		}),
		configmanager.WithOnError[AppConfig](func(err error) {
			log.Printf("config reload failed, still serving last good config: %v", err)
		}),
	)
	if err != nil {
		log.Fatal(err) // never start on a bad config
	}
	defer mgr.Close()

	// Shared read-only snapshot: cheap, must not be mutated.
	cfg := mgr.Get()
	fmt.Println(cfg.ListenAddr)

	// Fully isolated copy: safe to mutate.
	mine := mgr.GetDeepCopy()
	mine.Features = append(mine.Features, "local-only")
	fmt.Println(len(mgr.Get().Features), len(mine.Features))

	// Output:
	// :8080
	// 1 2
}
