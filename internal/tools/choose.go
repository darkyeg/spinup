package tools

import "strings"

const (
	osWindows = "windows"
	osDarwin  = "darwin"
)

type platform struct {
	os    string
	which func(program string) bool
}

type installer struct {
	program string
	args    []string
	asRoot  bool
}

func choose(t Tool, p platform) (installer, bool) {
	switch p.os {
	case osWindows:
		if t.Winget != "" {
			return wingetInstaller(t.Winget), true
		}
		if t.Powershell != "" {
			return installer{program: "powershell", args: []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", t.Powershell}}, true
		}
	case osDarwin:
		if t.Brew != "" && p.which("brew") {
			return installer{program: "brew", args: []string{"install", t.Brew}}, true
		}
		if t.Script != "" {
			return scriptInstaller(t.Script), true
		}
	default:
		if t.Apt != "" && p.which("apt-get") {
			return installer{program: "apt-get", args: append([]string{"install", "-y"}, strings.Fields(t.Apt)...), asRoot: true}, true
		}
		if t.Script != "" {
			return scriptInstaller(t.Script), true
		}
	}
	if t.Npm != "" && p.which("npm") {
		return installer{program: "npm", args: []string{"install", "-g", t.Npm}}, true
	}
	return installer{}, false
}

func wingetInstaller(id string) installer {
	return installer{program: "winget", args: []string{"install", "--id", id, "-e", "--silent",
		"--accept-package-agreements", "--accept-source-agreements"}}
}

func scriptInstaller(script string) installer {
	return installer{program: "sh", args: []string{"-c", script}}
}

// NixLine is the configuration.nix line that installs the tools on NixOS.
func NixLine(missing []Tool) string {
	var attrs []string
	for _, t := range missing {
		if t.Nix != "" {
			attrs = append(attrs, t.Nix)
		}
	}
	return "environment.systemPackages = with pkgs; [ " + strings.Join(attrs, " ") + " ];"
}
