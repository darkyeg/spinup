package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientNeverSendsKeysToARedirectTarget(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.Write([]byte("{}"))
	}))
	defer target.Close()
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, status)
			}))
			defer peer.Close()
			client := Client{HTTP: peer.Client(), Key: "management-sentinel", APIKey: "api-sentinel"}
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				var err error
				if method == http.MethodGet {
					err = client.Get(context.Background(), peer.URL, nil)
				} else {
					err = client.Post(context.Background(), peer.URL, struct{}{}, nil)
				}
				if !WasRefused(err) || reached.Load() != 0 {
					t.Fatalf("%s followed redirect: reached=%d, err=%v", method, reached.Load(), err)
				}
			}
		})
	}
}
