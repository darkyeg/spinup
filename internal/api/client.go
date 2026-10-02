package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/darkyeg/spinup/internal/config"
)

// Client calls a spinup service: the local one for the command line, peers for the service.
type Client struct {
	HTTP *http.Client
	// Key is the management password; public calls leave it empty.
	Key string
	// From and Hold describe the calling machine; the command line leaves them empty.
	From string
	Hold config.Hold
}

func (c Client) Get(ctx context.Context, url string, out any) error {
	return c.do(ctx, http.MethodGet, url, nil, out)
}

func (c Client) Post(ctx context.Context, url string, in, out any) error {
	return c.do(ctx, http.MethodPost, url, in, out)
}

func (c Client) do(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setIf(req.Header, KeyHeader, c.Key)
	setIf(req.Header, ForwardedHeader, c.From)
	setIf(req.Header, HoldHeader, string(c.Hold))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return failure(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Refused is a definite answer from the other side: the request arrived and was turned down.
// Any other error leaves open whether it took effect.
type Refused struct {
	Status  int
	Message string
}

func (r *Refused) Error() string { return r.Message }

func failure(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e Error
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return &Refused{Status: resp.StatusCode, Message: e.Error.Message}
	}
	return &Refused{Status: resp.StatusCode, Message: fmt.Sprintf("%s: %s", resp.Status, bytes.TrimSpace(data))}
}

// WasRefused reports whether err is a definite refusal rather than an unknown outcome.
func WasRefused(err error) bool {
	var r *Refused
	return errors.As(err, &r)
}

func setIf(h http.Header, name, value string) {
	if value != "" {
		h.Set(name, value)
	}
}
