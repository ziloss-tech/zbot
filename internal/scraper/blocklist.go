package scraper

import (
	"net/url"
	"strings"
)

// hardBlocklist — never scrape these domains.
// Includes login walls (useless without auth) and SSRF prevention targets.
var hardBlocklist = []string{
	// Social media login walls — content requires authentication.
	"facebook.com",
	"instagram.com",
	"linkedin.com",
	"tiktok.com",
	// SSRF prevention — block internal network access.
	"localhost",
	"127.0.0.1",
	"0.0.0.0",
	"[::1]",
	// AWS/GCP metadata endpoints.
	"169.254.169.254",
	"metadata.google.internal",
}

// internalCIDRPrefixes blocks private IP ranges (SSRF prevention).
var internalCIDRPrefixes = []string{
	"10.",
	"172.16.", "172.17.", "172.18.", "172.19.",
	"172.20.", "172.21.", "172.22.", "172.23.",
	"172.24.", "172.25.", "172.26.", "172.27.",
	"172.28.", "172.29.", "172.30.", "172.31.",
	"192.168.",
	"169.254.",
}

// IsBlocked checks if a URL's domain is on the blocklist or targets internal networks.
func IsBlocked(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true // malformed URLs are blocked
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return true
	}

	// Check hardcoded blocklist.
	for _, blocked := range hardBlocklist {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}

	// Check internal IP ranges (SSRF prevention).
	for _, prefix := range internalCIDRPrefixes {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}

	return false
}
