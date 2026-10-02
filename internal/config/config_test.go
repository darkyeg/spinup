package config

import (
	"strings"
	"testing"
)

func TestLoadRefusesAnUnsafeFailoverTime(t *testing.T) {
	t.Setenv("SPINUP_HOME", t.TempDir())
	for _, c := range []struct {
		seconds int
		ok      bool
	}{{180, true}, {150, true}, {149, false}, {30, false}} {
		cfg := Defaults()
		cfg.FailoverAfterSeconds = c.seconds
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
		_, err := Load()
		if (err == nil) != c.ok {
			t.Errorf("failover %ds: err %v, want ok=%v", c.seconds, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "at least 150") {
			t.Errorf("the error must say the minimum: %v", err)
		}
	}
}
