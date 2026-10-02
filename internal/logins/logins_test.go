package logins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func login(refreshed, token string) []byte {
	return []byte(`{"type":"codex","refresh_token":"` + token + `","last_refresh":"` + refreshed + `"}`)
}

func TestAnOlderCopyNeverReplacesANewerOne(t *testing.T) {
	older := login("2026-10-01T10:00:00Z", "a")
	newer := login("2026-10-01T11:00:00Z", "b")
	if !replaces(newer, older) {
		t.Fatal("a later refresh must replace an earlier one")
	}
	if replaces(older, newer) {
		t.Fatal("an earlier refresh must never replace a later one")
	}
	if replaces(newer, newer) {
		t.Fatal("identical copies don't replace each other")
	}
	if replaces([]byte(`{"refresh_token":"x"}`), older) {
		t.Fatal("an undatable copy must not replace a dated one")
	}
}

func TestRefreshTimeFormats(t *testing.T) {
	for _, data := range []string{
		`{"last_refresh":"2026-10-01T10:00:00Z"}`,
		`{"last_refresh":"2026-10-01T10:00:00.123+02:00"}`,
		`{"expired":"2026-10-01T10:00:00Z"}`,
		`{"expires_at":1790000000}`,
		`{"expires_at":1790000000000}`,
	} {
		if refreshedAt([]byte(data)).IsZero() {
			t.Errorf("no refresh time in %s", data)
		}
	}
}

func TestNamesFromTheNetworkStayInTheFolder(t *testing.T) {
	for _, bad := range []string{"", "../x.json", `..\x.json`, "a/b.json", "C:x.json", ".hidden.json", "x.txt"} {
		if validName(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if !validName("codex-user@example.com-pro.json") {
		t.Error("a normal login name must pass")
	}
}

func TestMerge(t *testing.T) {
	dir, removed := t.TempDir(), filepath.Join(t.TempDir(), "removed")
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("keep-newer.json", login("2026-10-01T12:00:00Z", "local"))
	write("take-newer.json", login("2026-10-01T09:00:00Z", "local"))
	write("gone.json", login("2026-10-01T09:00:00Z", "local"))

	m, err := Merge(dir, removed, []File{
		{Name: "keep-newer.json", Data: login("2026-10-01T10:00:00Z", "remote")},
		{Name: "take-newer.json", Data: login("2026-10-01T10:00:00Z", "remote")},
		{Name: "new.json", Data: login("2026-10-01T10:00:00Z", "remote")},
	}, Everything)
	if err != nil {
		t.Fatal(err)
	}
	if got := refreshToken(t, dir, "keep-newer.json"); got != "local" {
		t.Errorf("the newer local login was overwritten (token %q)", got)
	}
	if got := refreshToken(t, dir, "take-newer.json"); got != "remote" {
		t.Errorf("the newer remote login was not taken (token %q)", got)
	}
	if len(m.Written) != 2 || len(m.Removed) != 1 || m.Removed[0] != "gone.json" {
		t.Errorf("merged %+v", m)
	}
	if _, err := os.Stat(filepath.Join(removed, "gone.json")); err != nil {
		t.Error("a removed login must be kept in the removed folder")
	}

	if _, err := Merge(dir, removed, []File{
		{Name: "fine.json", Data: login("2026-10-01T10:00:00Z", "remote")},
		{Name: "../evil.json", Data: []byte(`{}`)},
	}, Some); err == nil {
		t.Error("a path outside the folder must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "fine.json")); err == nil {
		t.Error("a refused set must write nothing, not the logins before the bad one")
	}
	if m, _ := Merge(dir, removed, nil, Everything); len(m.Removed) != 0 {
		t.Error("an empty complete set must not remove anything")
	}
}

func refreshToken(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	var fields struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields.RefreshToken
}
