// Package scraper implements the anti-block web scraping subsystem.
// Proxy rotation, rate limiting, caching, headless browser, text extraction.
package scraper

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// ProxyPool rotates through a list of proxies round-robin.
// If no proxies configured, falls back to direct connection.
type ProxyPool struct {
	proxies []*url.URL
	idx     atomic.Uint64
}

// NewProxyPool creates a proxy pool from a list of proxy URL strings.
// Supported formats: http://user:pass@host:port, socks5://host:port
// If proxyURLs is empty or all are invalid, pool operates in direct mode.
func NewProxyPool(proxyURLs []string) *ProxyPool {
	p := &ProxyPool{}
	for _, raw := range proxyURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		p.proxies = append(p.proxies, u)
	}
	return p
}

// Next returns the next proxy in rotation, or nil for direct connection.
func (p *ProxyPool) Next() *url.URL {
	if len(p.proxies) == 0 {
		return nil
	}
	idx := p.idx.Add(1) - 1
	return p.proxies[idx%uint64(len(p.proxies))]
}

// Size returns the number of proxies in the pool.
func (p *ProxyPool) Size() int {
	return len(p.proxies)
}

// NewHTTPClient returns an http.Client configured with the next proxy.
// If no proxies available, returns a client with direct connection.
func (p *ProxyPool) NewHTTPClient(timeout time.Duration) *http.Client {
	proxy := p.Next()
	transport := &http.Transport{}

	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}
