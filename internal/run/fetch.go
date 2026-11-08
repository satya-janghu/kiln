package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// client gives up on an unreachable server quickly: the dial timeout is
// split across a host's addresses, so one dead address (seen with one of
// GitHub's) costs seconds, not minutes, before the next is tried.
var client = &http.Client{
	Timeout: 30 * time.Minute,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 40 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: time.Minute,
		ForceAttemptHTTP2:     true,
	},
}

// Get fetches a URL over HTTPS into memory (at most max bytes).
func (s *S) Get(url string, maxBytes int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kiln")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s; check the network connection: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

// Fetch downloads a URL into the run's /var/tmp directory and checks its
// sha256 when one is given. It returns the file's path.
func (s *S) Fetch(url, wantSHA string) (string, error) {
	dir, err := s.TempDir()
	if err != nil {
		return "", err
	}
	s.Say("Downloading %s", url)
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "kiln")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download %s; check the network connection: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s returned HTTP %d", url, resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, "dl-*-"+filepath.Base(req.URL.Path))
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("downloading %s failed: %w", url, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	got := hex.EncodeToString(h.Sum(nil))
	s.Log("downloaded %s (sha256 %s)", url, got)
	if wantSHA != "" && got != wantSHA {
		return "", fmt.Errorf("%s doesn't match its expected checksum (got %s); the file may have changed upstream, so kiln needs an update", url, got)
	}
	return f.Name(), nil
}
