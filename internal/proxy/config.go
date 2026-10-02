// Package proxy runs CLIProxyAPI: its config, its process, its release downloads and its
// management API.
package proxy

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkyeg/spinup"
	"github.com/darkyeg/spinup/internal/atomicfile"
	"github.com/darkyeg/spinup/internal/config"
)

func render(template, host string, port int, authDir string, s config.Secrets) string {
	return strings.NewReplacer(
		"{{HOST}}", host,
		"{{AUTH_DIR}}", filepath.ToSlash(authDir), // YAML needs no escapes for forward slashes
		"{{PORT}}", strconv.Itoa(port),
		"{{API_KEY}}", s.APIKey,
		"{{SECRET_KEY}}", s.ManagementPassword,
	).Replace(template)
}

// WriteConfig keeps CLIProxyAPI on 127.0.0.1: spinup is the only way in.
func WriteConfig(c config.Config, s config.Secrets) error {
	text := render(spinup.ProxyConfigTemplate, "127.0.0.1", c.ProxyPort, c.AuthDir, s)
	return atomicfile.Write(c.ProxyConfig(), []byte(text), 0o600)
}
