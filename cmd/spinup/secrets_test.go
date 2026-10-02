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
	"github.com/darkyeg/spinup/internal/tailnet"
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

func TestAnExplicitAPIKeyReplacesTheStoredOne(t *testing.T) {
	cfg := config.Defaults()
	cfg.Hold = config.HoldNever
	cfg.ProxyDir = t.TempDir()
	if err := config.SaveSecrets(cfg, config.Secrets{APIKey: "old"}); err != nil {
		t.Fatal(err)
	}

	kept, err := machineKeys(cfg, tailnet.Status{}, serviceRequest{})
	if err != nil || kept != "old" {
		t.Fatalf("without a given key got (%q, %v), want the stored one", kept, err)
	}
	given, err := machineKeys(cfg, tailnet.Status{}, serviceRequest{apiKey: "new"})
	if err != nil || given != "new" {
		t.Fatalf("with a given key got (%q, %v)", given, err)
	}
	if s, err := config.LoadSecrets(cfg); err != nil || s.APIKey != "new" {
		t.Fatalf("stored = %+v, %v", s, err)
	}
}

func TestOnlyTheKeyAMachineUsesReplacesItsStoredKeys(t *testing.T) {
	tests := []struct {
		hold config.Hold
		req  serviceRequest
		want bool
	}{
		{config.HoldNever, serviceRequest{apiKey: "k"}, true},
		{config.HoldNever, serviceRequest{password: "p"}, false},
		{config.HoldStandby, serviceRequest{password: "p"}, true},
		{config.HoldStandby, serviceRequest{apiKey: "k"}, false},
		{config.HoldHub, serviceRequest{apiKey: "k", password: "p"}, false},
	}
	for _, tt := range tests {
		if got := tt.req.replacesKeys(tt.hold); got != tt.want {
			t.Errorf("%s with %+v: got %v", tt.hold, tt.req, got)
		}
	}
}
