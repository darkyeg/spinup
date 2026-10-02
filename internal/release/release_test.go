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
