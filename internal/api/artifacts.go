package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/darkyeg/spinup/internal/library"
)

func (c Client) GetArtifact(ctx context.Context, base, digest string) ([]byte, error) {
	return c.artifact(ctx, http.MethodGet, base, digest, nil)
}

func (c Client) PutArtifact(ctx context.Context, base, digest string, data []byte) error {
	_, err := c.artifact(ctx, http.MethodPost, base, digest, data)
	return err
}

func (c Client) artifact(ctx context.Context, method, base, digest string, data []byte) ([]byte, error) {
	if !library.ValidDigest(digest) || len(data) > library.ArtifactLimit {
		return nil, errors.New("invalid package artifact")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+PathLibraryArtifact+digest, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/zip")
	if method == http.MethodPost {
		req.Header.Set("Expect", "100-continue")
	}
	setIf(req.Header, APIKeyHeader, c.APIKey)
	setIf(req.Header, ForwardedHeader, c.From)
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, failure(resp)
	}
	if method != http.MethodGet {
		return nil, nil
	}
	result, err := io.ReadAll(io.LimitReader(resp.Body, library.ArtifactLimit+1))
	if err != nil {
		return nil, err
	}
	if len(result) > library.ArtifactLimit {
		return nil, errors.New("package artifact exceeds transfer limit")
	}
	return result, nil
}
