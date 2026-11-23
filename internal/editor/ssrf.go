package editor

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked means an address is not allowed because it is private, loopback, link-local or otherwise internal.
var ErrBlocked = errors.New("editor: address not allowed (private or internal)")

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// IsBlocked reports whether ip is an address a user-supplied URL must not reach.
func IsBlocked(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

// ParseURL checks that raw is an http(s) URL with a host and no credentials, and returns it without a
// trailing slash. It does not resolve names: the connection itself is checked by NewClient.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("the editor URL must look like https://editor.example.com")
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

// NewClient returns an HTTP client for talking to the editor server. Unless allowPrivate is set it refuses to
// connect to private and internal addresses. The check runs on the address actually dialled, after name
// resolution, so a name that later resolves somewhere else (DNS rebinding) is caught too. It never follows
// redirects and never uses a proxy from the environment.
func NewClient(allowPrivate bool, timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlocked
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || IsBlocked(ip) {
				return ErrBlocked
			}
			return nil
		}
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{DialContext: d.DialContext, Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// SameOrigin reports whether raw is an absolute URL on the same scheme, host and port as base.
func SameOrigin(base *url.URL, raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User == nil && strings.EqualFold(u.Scheme, base.Scheme) && strings.EqualFold(u.Host, base.Host)
}
