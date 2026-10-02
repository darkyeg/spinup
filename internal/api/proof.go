package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/darkyeg/spinup/internal/config"
)

// NonceParam carries the caller's random challenge on PathLeader.
const NonceParam = "nonce"

// Proof says which secret a device showed it knows.
type Proof int

const (
	Unproven Proof = iota
	KnowsAPIKey
	KnowsPassword
)

const (
	apiKeyLabel   = "spinup/leader/api-key/"
	passwordLabel = "spinup/leader/password/"
)

// newNonce is a fresh challenge.
func newNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// mac binds the proof to the answering machine's name, so a device can't pass off another's proof.
func mac(label, secret, name, nonce string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(label + name + "/" + nonce))
	return hex.EncodeToString(h.Sum(nil))
}

func matches(label, secret, name, nonce, proof string) bool {
	return secret != "" && nonce != "" && hmac.Equal([]byte(mac(label, secret, name, nonce)), []byte(proof))
}

// Prove answers nonce with every proof the secrets allow.
func (l Leader) Prove(s config.Secrets, nonce string) Leader {
	if nonce == "" {
		return l
	}
	if s.APIKey != "" {
		l.APIKeyProof = mac(apiKeyLabel, s.APIKey, l.Name, nonce)
	}
	if s.ManagementPassword != "" {
		l.PasswordProof = mac(passwordLabel, s.ManagementPassword, l.Name, nonce)
	}
	return l
}

// proofFor is the strongest proof in l that checks out against s for the machine called name.
func (l Leader) proofFor(s config.Secrets, name, nonce string) Proof {
	switch {
	case l.Name != name:
		return Unproven
	case matches(passwordLabel, s.ManagementPassword, name, nonce, l.PasswordProof):
		return KnowsPassword
	case matches(apiKeyLabel, s.APIKey, name, nonce, l.APIKeyProof):
		return KnowsAPIKey
	}
	return Unproven
}
