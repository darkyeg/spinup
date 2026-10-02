package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
)

var accountsSecrets = config.Secrets{APIKey: "sk-real", ManagementPassword: "the-password"}

func holder(t *testing.T, name string, secrets config.Secrets, sawKey *atomic.Bool) holderCandidate {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(api.PathLeader, func(w http.ResponseWriter, r *http.Request) {
		l := api.Leader{Name: name, Hold: config.HoldHub, Leading: true}
		_ = json.NewEncoder(w).Encode(l.Prove(secrets, r.URL.Query().Get(api.NonceParam)))
	})
	mux.HandleFunc(api.PathSecrets, func(w http.ResponseWriter, r *http.Request) {
		if sawKey != nil && r.Header.Get(api.KeyHeader) != "" {
			sawKey.Store(true)
		}
		if r.Header.Get(api.KeyHeader) != secrets.ManagementPassword {
			http.Error(w, `{"error":{"message":"wrong key"}}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(secrets)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return holderCandidate{name: name, base: srv.URL}
}

func TestThePasswordGoesOnlyToADeviceThatProvesItKnowsIt(t *testing.T) {
	var spooferSawKey atomic.Bool
	spoofer := holder(t, "spoof", config.Secrets{APIKey: "x", ManagementPassword: "other"}, &spooferSawKey)
	genuine := holder(t, "hub", accountsSecrets, nil)

	got, err := secretsFrom(context.Background(), []holderCandidate{spoofer, genuine}, "the-password")
	if err != nil || got != accountsSecrets {
		t.Fatalf("got (%+v, %v), want the hub's secrets", got, err)
	}
	if spooferSawKey.Load() {
		t.Fatal("the password went to a device that doesn't know it")
	}
}

func TestAWrongPasswordIsReportedWithoutBeingSent(t *testing.T) {
	var sawKey atomic.Bool
	spoofer := holder(t, "spoof", config.Secrets{APIKey: "x", ManagementPassword: "other"}, &sawKey)
	_, err := secretsFrom(context.Background(), []holderCandidate{spoofer}, "the-password")
	if err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Errorf("got %v, want a wrong-password hint", err)
	}
	if sawKey.Load() {
		t.Fatal("the password went to a device that doesn't know it")
	}
}

func TestNobodyHoldsTheAccounts(t *testing.T) {
	if _, err := secretsFrom(context.Background(), nil, "pw"); err == nil || !strings.Contains(err.Error(), "install the hub first") {
		t.Errorf("got %v", err)
	}
}
