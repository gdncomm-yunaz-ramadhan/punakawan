// Package loopback holds the request and bind-address validation shared
// by every loopback-only HTTP server in Punakawan (the Panel, and the
// punokawan-14yn.17 daemon transport): reject unexpected Host/Origin
// headers and refuse to bind anywhere but a loopback address. Extracted
// from internal/panel/server so the daemon transport does not
// reimplement the same DNS-rebinding and cross-origin defenses.
package loopback

import (
	"fmt"
	"net"
	"strings"
)

// Hosts is what Host/Origin values a request is allowed to carry. Both
// "[::1]" and "::1" are listed: net.SplitHostPort strips the brackets
// from a bracketed IPv6 literal, so the host-only value seen after
// splitting a "[::1]:port" Host header is "::1", not "[::1]".
var Hosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"[::1]":     true,
	"::1":       true,
}

// ValidateHost rejects a request whose Host header does not name a
// loopback address, regardless of what interface accepted the TCP
// connection - this is the DNS-rebinding defense.
func ValidateHost(host string) error {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	if !Hosts[h] {
		return fmt.Errorf("loopback: unexpected Host %q", host)
	}
	return nil
}

// ValidateOrigin rejects a cross-origin request: a page loaded from any
// other origin (including another localhost port) must not be able to
// call this API using the browser's ambient credentials. A missing
// Origin header (same-origin navigations, curl, non-browser clients) is
// allowed through - only a mismatching Origin is rejected.
func ValidateOrigin(origin, host string) error {
	if origin == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(origin, "http://"), "/")
	trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "https://"), "/")
	if h, _, err := net.SplitHostPort(host); err == nil {
		if oh, _, err := net.SplitHostPort(trimmed); err == nil {
			trimmed = oh
			host = h
		}
	}
	if !Hosts[trimmed] {
		return fmt.Errorf("loopback: unexpected Origin %q", origin)
	}
	return nil
}

// Listener resolves host to a loopback-only bind address, rejecting
// non-loopback binding rather than silently binding somewhere else.
// host is expected to be "127.0.0.1", "localhost", or "::1".
func Listener(host, port string) (net.Listener, error) {
	resolved := host
	if resolved == "" {
		resolved = "127.0.0.1"
	}
	ip := net.ParseIP(resolved)
	isLoopback := resolved == "localhost" || (ip != nil && ip.IsLoopback())
	if !isLoopback {
		return nil, fmt.Errorf("loopback: refusing non-loopback bind address %q", host)
	}
	return net.Listen("tcp", net.JoinHostPort(resolved, port))
}
