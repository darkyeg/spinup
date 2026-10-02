package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/darkyeg/spinup/internal/api"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(status int, body string, header ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

var refused = &net.OpError{Op: "dial", Err: errors.New("connection refused")}

func holding(base roundTrip, route func() (destination, bool)) holdingTransport {
	return holdingTransport{base: base, route: route, self: func() string { return "me" }, within: func() time.Duration { return time.Minute }, every: time.Millisecond}
}

func leaderAt(host string) func() (destination, bool) {
	return func() (destination, bool) { return destination{url: &url.URL{Host: host}, toPeer: true}, true }
}

func postRequest(t *testing.T, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://localhost:8317/v1/messages", body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestARefusedConnectionIsRetriedWithTheSameBody(t *testing.T) {
	var bodies []string
	tr := holding(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(data))
		if len(bodies) == 1 {
			return nil, refused
		}
		return reply(200, "ok"), nil
	}, leaderAt("leader:8317"))

	resp, err := tr.RoundTrip(postRequest(t, strings.NewReader("prompt")))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("got %v, %v; want the retry to succeed", resp, err)
	}
	if len(bodies) != 2 || bodies[0] != "prompt" || bodies[1] != "prompt" {
		t.Fatalf("bodies sent: %q, want the prompt twice", bodies)
	}
}

func TestAPeerThatNoLongerHoldsTheAccountsIsRetriedUntilAnotherDoes(t *testing.T) {
	hosts := []string{"old:8317", "old:8317", "new:8317"}
	var tried []string
	tr := holding(func(r *http.Request) (*http.Response, error) {
		tried = append(tried, r.URL.Host)
		if r.URL.Host == "old:8317" {
			return reply(503, "not me", api.NotHoldingHeader, "1"), nil
		}
		return reply(200, "served"), nil
	}, func() (destination, bool) {
		host := hosts[0]
		if len(hosts) > 1 {
			hosts = hosts[1:]
		}
		return destination{url: &url.URL{Host: host}, toPeer: true}, true
	})

	resp, err := tr.RoundTrip(postRequest(t, strings.NewReader("prompt")))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("got %v, %v; want the new leader's answer", resp, err)
	}
	if strings.Join(tried, ",") != "old:8317,old:8317,new:8317" {
		t.Fatalf("tried %v", tried)
	}
}

func TestARequestWaitsForALeaderToAppear(t *testing.T) {
	looks := 0
	tr := holding(func(*http.Request) (*http.Response, error) { return reply(200, "ok"), nil },
		func() (destination, bool) {
			looks++
			return destination{url: &url.URL{Host: "leader:8317"}, toPeer: true}, looks > 3
		})
	resp, err := tr.RoundTrip(postRequest(t, nil))
	if err != nil || resp.StatusCode != 200 || looks != 4 {
		t.Fatalf("got %v, %v after %d looks; want an answer on the fourth", resp, err, looks)
	}
}

func TestWaitingForALeaderEndsAtTheLimit(t *testing.T) {
	tr := holding(nil, func() (destination, bool) { return destination{}, false })
	tr.within = func() time.Duration { return 20 * time.Millisecond }
	if _, err := tr.RoundTrip(postRequest(t, nil)); !errors.Is(err, errNoLeader) {
		t.Fatalf("got %v, want errNoLeader", err)
	}
}

func TestWaitingForALeaderEndsWhenTheClientLeaves(t *testing.T) {
	tr := holding(nil, func() (destination, bool) { return destination{}, false })
	req := postRequest(t, nil)
	ctx, leave := context.WithCancel(context.Background())
	leave()
	if _, err := tr.RoundTrip(req.WithContext(ctx)); !errors.Is(err, errNoLeader) {
		t.Fatalf("got %v, want the wait to end at once", err)
	}
}

func TestARequestThatCannotBeReplayedIsNotSentTwice(t *testing.T) {
	sent := 0
	tr := holding(func(r *http.Request) (*http.Response, error) {
		sent++
		return reply(503, "not me", api.NotHoldingHeader, "1"), nil
	}, leaderAt("old:8317"))

	resp, err := tr.RoundTrip(postRequest(t, io.NopCloser(strings.NewReader("a huge prompt"))))
	if err != nil || resp.StatusCode != 503 || sent != 1 {
		t.Fatalf("got %v, %v after %d sends; want the 503 passed on after one", resp, err, sent)
	}
}

func TestAnAnswerIsNeverRetriedOnceItExists(t *testing.T) {
	sent := 0
	tr := holding(func(*http.Request) (*http.Response, error) {
		sent++
		return reply(503, "overloaded"), nil
	}, leaderAt("leader:8317"))
	resp, err := tr.RoundTrip(postRequest(t, strings.NewReader("prompt")))
	if err != nil || resp.StatusCode != 503 || sent != 1 {
		t.Fatalf("got %v, %v after %d sends; a 503 without the not-holding mark is the proxy's own answer", resp, err, sent)
	}
}

func TestAForwardedRequestIsNeverPassedOnAndNeverWaits(t *testing.T) {
	for name, route := range map[string]func() (destination, bool){
		"no leader known": func() (destination, bool) { return destination{}, false },
		"another leader":  leaderAt("other:8317"),
	} {
		t.Run(name, func(t *testing.T) {
			tr := holding(func(*http.Request) (*http.Response, error) {
				t.Fatal("a request from a peer reached another machine")
				return nil, nil
			}, route)
			req := postRequest(t, nil)
			req.Header.Set(api.ForwardedHeader, "peer")
			if _, err := tr.RoundTrip(req); !errors.Is(err, errNotHolding) {
				t.Fatalf("got %v, want errNotHolding", err)
			}
		})
	}
}

func TestTheForwardedMarkNamesThisMachineOnlyTowardAPeer(t *testing.T) {
	var mark string
	base := func(r *http.Request) (*http.Response, error) {
		mark = r.Header.Get(api.ForwardedHeader)
		return reply(200, "ok"), nil
	}
	if _, err := holding(base, leaderAt("leader:8317")).RoundTrip(postRequest(t, nil)); err != nil || mark != "me" {
		t.Errorf("toward a peer the mark is %q (%v), want this machine's name", mark, err)
	}
	local := func() (destination, bool) { return destination{url: &url.URL{Host: "127.0.0.1:8327"}}, true }
	req := postRequest(t, nil)
	req.Header.Set(api.ForwardedHeader, "peer")
	if _, err := holding(base, local).RoundTrip(req); err != nil || mark != "" {
		t.Errorf("toward the local proxy the mark is %q (%v), want none", mark, err)
	}
}

func TestARequestIsCountedUntilItsAnswerIsClosed(t *testing.T) {
	var a activity
	tr := holding(func(*http.Request) (*http.Response, error) { return reply(200, "streaming"), nil },
		func() (destination, bool) {
			return destination{url: &url.URL{Host: "127.0.0.1:8327"}, end: beginOn(&a)}, true
		})
	resp, err := tr.RoundTrip(postRequest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := a.inFlight(); got != 1 {
		t.Fatalf("while the answer streams: %d running, want 1", got)
	}
	resp.Body.Close()
	if got := a.inFlight(); got != 0 {
		t.Fatalf("after the answer closed: %d running, want 0", got)
	}
}

func TestARefusedConnectionDoesNotLeaveARequestCounted(t *testing.T) {
	var a activity
	tr := holding(func(*http.Request) (*http.Response, error) { return nil, refused },
		func() (destination, bool) {
			return destination{url: &url.URL{Host: "127.0.0.1:8327"}, end: beginOn(&a)}, true
		})
	tr.within = func() time.Duration { return 10 * time.Millisecond }
	_, _ = tr.RoundTrip(postRequest(t, bytes.NewReader(nil)))
	if got := a.inFlight(); got != 0 {
		t.Fatalf("%d requests still counted after the connection failed", got)
	}
}

func TestKeepBodyReplaysSmallBodiesAndStreamsLargeOnes(t *testing.T) {
	small := postRequest(t, io.NopCloser(strings.NewReader("small")))
	if err := keepBody(small); err != nil || small.GetBody == nil {
		t.Fatalf("a small body must be replayable (%v)", err)
	}
	for range 2 {
		body, _ := small.GetBody()
		if data, _ := io.ReadAll(body); string(data) != "small" {
			t.Fatalf("replayed %q", data)
		}
	}
	large := postRequest(t, io.NopCloser(io.LimitReader(zeros{}, replayBodyUpTo+10)))
	large.ContentLength = -1
	if err := keepBody(large); err != nil || large.GetBody != nil {
		t.Fatalf("a body over the limit must not be replayable (%v)", err)
	}
	if n, _ := io.Copy(io.Discard, large.Body); n != replayBodyUpTo+10 {
		t.Fatalf("the large body lost bytes: read %d", n)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func beginOn(a *activity) func() {
	end, _ := a.begin()
	return end
}
