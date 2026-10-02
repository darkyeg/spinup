package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/darkyeg/spinup/internal/source"
)

func platformWith(os string, present ...string) platform {
	return platform{os: os, which: func(program string) bool {
		for _, p := range present {
			if p == program {
				return true
			}
		}
		return false
	}}
}

func TestChooseInstaller(t *testing.T) {
	full := Tool{Winget: "A.B", Powershell: "irm x | iex", Brew: "b", Apt: "p q", Script: "curl x | sh", Npm: "@x/y"}
	tests := []struct {
		name     string
		tool     Tool
		platform platform
		want     installer
		found    bool
	}{
		{"windows prefers winget", full, platformWith("windows", "npm"), wingetInstaller("A.B"), true},
		{"windows falls back to powershell", Tool{Powershell: "irm x | iex"}, platformWith("windows"),
			installer{program: "powershell", args: []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm x | iex"}}, true},
		{"windows npm when nothing else", Tool{Npm: "@x/y", Brew: "b"}, platformWith("windows", "npm"),
			installer{program: "npm", args: []string{"install", "-g", "@x/y"}}, true},
		{"windows npm missing", Tool{Npm: "@x/y"}, platformWith("windows"), installer{}, false},
		{"mac brew when present", full, platformWith("darwin", "brew"), installer{program: "brew", args: []string{"install", "b"}}, true},
		{"mac script without brew", full, platformWith("darwin"), scriptInstaller("curl x | sh"), true},
		{"mac npm without brew or script", Tool{Brew: "b", Npm: "@x/y"}, platformWith("darwin", "npm"),
			installer{program: "npm", args: []string{"install", "-g", "@x/y"}}, true},
		{"linux apt as root", full, platformWith("linux", "apt-get"),
			installer{program: "apt-get", args: []string{"install", "-y", "p", "q"}, asRoot: true}, true},
		{"linux script without apt-get", full, platformWith("linux"), scriptInstaller("curl x | sh"), true},
		{"linux nothing fits", Tool{Winget: "A.B"}, platformWith("linux", "npm"), installer{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, found := choose(tc.tool, tc.platform)
			if found != tc.found || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("choose = %+v, %v; want %+v, %v", got, found, tc.want, tc.found)
			}
		})
	}
}

func TestInstalledIfAnyCheckCommandIsFound(t *testing.T) {
	fd := Tool{Check: []string{"fd", "fdfind"}}
	if !installed(fd, platformWith("linux", "fdfind")) {
		t.Error("fdfind alone should count")
	}
	if installed(fd, platformWith("linux", "rg")) {
		t.Error("no check command is on PATH")
	}
}

func TestNixLineListsOnlyToolsWithANixName(t *testing.T) {
	got := NixLine([]Tool{{Nix: "git"}, {Name: "nope"}, {Nix: "ripgrep"}})
	want := "environment.systemPackages = with pkgs; [ git ripgrep ];"
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestLoadReadsToolsJSON(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(source.At(dir)); err == nil {
		t.Error("a source without tools.json must fail")
	}
	body := `{"_comment":"x","tools":[{"name":"fd","check":["fd","fdfind"],"apt":"fd-find","nix":"fd"}]}`
	if err := os.WriteFile(filepath.Join(dir, "tools.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(source.At(dir))
	want := []Tool{{Name: "fd", Check: []string{"fd", "fdfind"}, Apt: "fd-find", Nix: "fd"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %+v, %v", got, err)
	}
}
