package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestLoginStatesReturnsTheDecodedAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files" || r.Header.Get("Authorization") != "Bearer test-password" {
			t.Error("the management lookup used the wrong route or key")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"files":[{"name":"acct.json","status":"error","status_message":"invalid_grant during refresh","unavailable":true}]}`))
	}))
	defer srv.Close()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	states, err := LoginStates(context.Background(), number, "test-password")
	if err != nil || len(states) != 1 || states[0].Name != "acct.json" || !states[0].Refused() {
		t.Fatalf("management lookup returned %d accounts, error %v", len(states), err)
	}
}
