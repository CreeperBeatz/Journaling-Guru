package handlers

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP returns the best guess for the caller's IP, preferring
// X-Forwarded-For when the request arrived through a trusted proxy
// (Caddy, in our prod topology). Falls back to RemoteAddr otherwise.
//
// The first non-empty entry of XFF wins — that's the original client.
// Spoofing is mitigated by terminating XFF at Caddy in prod.
func clientIP(r *http.Request) netip.Addr {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			s := strings.TrimSpace(part)
			if a, err := netip.ParseAddr(s); err == nil {
				return a
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a
	}
	return netip.Addr{}
}

// userAgent returns at most 512 chars to keep the audit columns bounded.
func userAgent(r *http.Request) string {
	ua := r.Header.Get("User-Agent")
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return ua
}
