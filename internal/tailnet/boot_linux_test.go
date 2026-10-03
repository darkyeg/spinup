package tailnet

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStartAtBootOnlyRepairsUnreadyTailscale(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, active string
		repair                bool
	}{
		{"already ready", "enabled", "active", false},
		{"disabled", "disabled", "active", true},
		{"stopped", "enabled", "inactive", true},
		{"only enabled until reboot", "enabled-runtime", "active", true},
		{"unknown service state", "unknown", "active", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			repaired := filepath.Join(t.TempDir(), "repaired")
			programs := map[string]string{
				"systemctl": `#!/bin/sh
case "$1" in
is-enabled) printf '%s\n' "$SPINUP_TEST_ENABLED"; [ "$SPINUP_TEST_ENABLED" = enabled ] || [ "$SPINUP_TEST_ENABLED" = enabled-runtime ] ;;
is-active) [ "$SPINUP_TEST_ACTIVE" = active ] ;;
enable) : > "$SPINUP_TEST_REPAIRED" ;;
*) exit 1 ;;
esac
`,
				"sudo": "#!/bin/sh\nexec \"$@\"\n",
			}
			for name, script := range programs {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("SPINUP_TEST_ENABLED", tc.enabled)
			t.Setenv("SPINUP_TEST_ACTIVE", tc.active)
			t.Setenv("SPINUP_TEST_REPAIRED", repaired)
			if err := startAtBoot(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(repaired)
			if got := err == nil; got != tc.repair {
				t.Fatalf("ran the privileged repair = %v, want %v", got, tc.repair)
			}
		})
	}
}
