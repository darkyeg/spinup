package release

import "testing"

func TestChecksumFor(t *testing.T) {
	sums := "abc  spinup_linux_amd64\ndef *CLIProxyAPI_1_windows_amd64.zip\n"
	cases := map[string]string{"spinup_linux_amd64": "abc", "CLIProxyAPI_1_windows_amd64.zip": "def", "missing": ""}
	for name, want := range cases {
		if got := checksumFor(sums, name); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.2.0", "1.1.9", true},
		{"v1.10.0", "1.9.0", true},
		{"1.0.1", "1.0", true},
		{"1.0", "1.0.0", false},
		{"1.2.0", "1.2.0", false},
		{"1.1.0", "1.2.0", false},
		{"0.9.0", "v1.0.0", false},
		{"2.0.0", "2.0.0-rc1", false},
		{"2.0.1", "2.0.0+build5", true},
		{"latest", "1.0.0", false},
		{"1.0.0", "dev", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
