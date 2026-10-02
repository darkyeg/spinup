package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkyeg/spinup/internal/atomicfile"
)

// Secrets are the keys in secrets.json. The management password opens the dashboard and is the
// key machines that can hold use with each other; the API key is all a machine needs to use the accounts.
type Secrets struct {
	APIKey             string `json:"api_key"`
	ManagementPassword string `json:"management_password"`
}

func (c Config) SecretsPath() string { return filepath.Join(c.ProxyDir, "secrets.json") }

func LoadSecrets(c Config) (Secrets, error) {
	var s Secrets
	data, err := os.ReadFile(c.SecretsPath())
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s: %w", c.SecretsPath(), err)
	}
	if s.APIKey == "" || (c.Hold.CanHold() && s.ManagementPassword == "") {
		return s, fmt.Errorf("%s: missing api_key or management_password", c.SecretsPath())
	}
	return s, nil
}

func SaveSecrets(c Config, s Secrets) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	return atomicfile.Write(c.SecretsPath(), append(data, '\n'), 0o600)
}

func NewSecrets() Secrets {
	return Secrets{APIKey: "sk-" + randomHex(24), ManagementPassword: randomHex(24)}
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
