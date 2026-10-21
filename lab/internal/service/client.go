package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Fetcher reuses one transport. Every response body has exactly one owner,
// including error-status bodies; reading EOF permits HTTP/1.1 connection reuse.
type Fetcher struct {
	Client    *http.Client
	Transport *http.Transport
}

func NewFetcher(maxConns int) *Fetcher {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxConnsPerHost:       maxConns,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   maxConns,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &Fetcher{Client: &http.Client{Transport: tr}, Transport: tr}
}

// Fetch limits response bytes. An oversized response is closed before EOF,
// intentionally sacrificing reuse rather than consuming unbounded bandwidth.
func (f *Fetcher) Fetch(ctx context.Context, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	const maxBody = 1 << 20
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return n, err
	}
	if n > maxBody {
		return n, fmt.Errorf("response exceeds %d bytes", maxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return n, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return n, nil
}

func (f *Fetcher) Close() { f.Transport.CloseIdleConnections() }
