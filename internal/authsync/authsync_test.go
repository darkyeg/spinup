package authsync

import (
	"os"
	"path/filepath"
	"testing"
)

func login(refreshed, token string) []byte {
	return []byte(`{"type":"codex","refresh_token":"` + token + `","last_refresh":"` + refreshed + `"}`)
}

func TestNewerNeverGoesBack(t *testing.T) {
	old := login("2026-10-01T10:00:00Z", "a")
	newer := login("2026-10-01T11:00:00Z", "b")
	if !Newer(newer, old) {
		t.Fatal("a newer refresh must replace an older one")
	}
	if Newer(old, newer) {
		t.Fatal("an older refresh must never replace a newer one")
	}
	if Newer(newer, newer) {
		t.Fatal("identical copies are not newer")
	}
	if Newer([]byte(`{"refresh_token":"x"}`), old) {
		t.Fatal("a copy without a timestamp must not replace one we can date")
	}
}

func TestStampFormats(t *testing.T) {
	for _, data := range []string{
		`{"last_refresh":"2026-10-01T10:00:00Z"}`,
		`{"last_refresh":"2026-10-01T10:00:00.123+02:00"}`,
		`{"expired":"2026-10-01T10:00:00Z"}`,
		`{"expires_at":1790000000}`,
		`{"expires_at":1790000000000}`,
	} {
		if Stamp([]byte(data)).IsZero() {
			t.Errorf("no stamp in %s", data)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", "../x.json", `..\x.json`, "a/b.json", "C:x.json", ".hidden.json", "x.txt"} {
		if ValidName(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if !ValidName("codex-user@example.com-pro.json") {
		t.Error("a normal login name must pass")
	}
}

func TestApply(t *testing.T) {
	dir, removed := t.TempDir(), filepath.Join(t.TempDir(), "removed")
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("keep-newer.json", login("2026-10-01T12:00:00Z", "local"))
	write("take-newer.json", login("2026-10-01T09:00:00Z", "local"))
	write("gone.json", login("2026-10-01T09:00:00Z", "local"))

	res, err := Apply(dir, removed, []File{
		{Name: "keep-newer.json", Data: login("2026-10-01T10:00:00Z", "remote")},
		{Name: "take-newer.json", Data: login("2026-10-01T10:00:00Z", "remote")},
		{Name: "new.json", Data: login("2026-10-01T10:00:00Z", "remote")},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := RefreshToken(read(t, dir, "keep-newer.json")); got != "local" {
		t.Errorf("newer local login was overwritten (token %q)", got)
	}
	if got := RefreshToken(read(t, dir, "take-newer.json")); got != "remote" {
		t.Errorf("newer remote login was not taken (token %q)", got)
	}
	if len(res.Written) != 2 || len(res.Removed) != 1 || res.Removed[0] != "gone.json" {
		t.Errorf("unexpected result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(removed, "gone.json")); err != nil {
		t.Error("removed login must be kept in the removed folder")
	}

	if _, err := Apply(dir, removed, []File{{Name: "../evil.json", Data: []byte(`{}`)}}, false); err == nil {
		t.Error("a path outside the folder must be refused")
	}
	if res, _ := Apply(dir, removed, nil, true); len(res.Removed) != 0 {
		t.Error("an empty full bundle must not remove anything")
	}
}

func read(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
