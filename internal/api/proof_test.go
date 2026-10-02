package api

import (
	"testing"

	"github.com/darkyeg/spinup/internal/config"
)

func TestProofs(t *testing.T) {
	both := config.Secrets{APIKey: "key", ManagementPassword: "pw"}
	apiOnly := config.Secrets{APIKey: "key"}
	nonce := newNonce()
	if nonce == newNonce() {
		t.Fatal("nonces repeat")
	}
	answer := Leader{Name: "hub"}.Prove(both, nonce)
	renamed := answer
	renamed.Name = "phone"

	cases := []struct {
		name    string
		answer  Leader
		secret  config.Secrets
		machine string
		nonce   string
		want    Proof
	}{
		{"the password proves a holder", answer, both, "hub", nonce, KnowsPassword},
		{"a machine with only the API key sees the API key proof", answer, apiOnly, "hub", nonce, KnowsAPIKey},
		{"a proof for another nonce is worthless", answer, both, "hub", newNonce(), Unproven},
		{"an answer without proofs proves nothing", Leader{Name: "hub", Leading: true}, both, "hub", nonce, Unproven},
		{"the wrong password proves nothing", answer, config.Secrets{APIKey: "x", ManagementPassword: "y"}, "hub", nonce, Unproven},
		{"an API-key-only answer never proves the password",
			Leader{Name: "hub"}.Prove(apiOnly, nonce), both, "hub", nonce, KnowsAPIKey},
		{"empty secrets never match an empty proof", Leader{Name: "hub"}, config.Secrets{}, "hub", nonce, Unproven},
		{"no nonce, no proof", Leader{Name: "hub"}.Prove(both, ""), both, "hub", "", Unproven},
		{"another machine's proof, passed on, proves nothing", answer, both, "phone", nonce, Unproven},
		{"another machine's proof, renamed, proves nothing", renamed, both, "phone", nonce, Unproven},
		{"the API-key proof is not a password proof",
			Leader{Name: "hub", PasswordProof: answer.APIKeyProof}, config.Secrets{ManagementPassword: "key"}, "hub", nonce, Unproven},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.answer.proofFor(c.secret, c.machine, c.nonce); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
