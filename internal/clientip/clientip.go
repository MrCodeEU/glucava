// Package clientip finds the real client address behind a reverse proxy.
// Forwarding headers are believed only when the direct peer is a trusted proxy,
// because any client can send them.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolver holds the proxies whose forwarding headers are believed.
// The zero value trusts nobody and always returns the direct peer.
type Resolver struct {
	trusted []netip.Prefix
}

// Parse builds a Resolver from a comma-separated list of IPs and CIDRs.
func Parse(list string) (*Resolver, error) {
	r := &Resolver{}
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			r.trusted = append(r.trusted, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("clientip: %q is not an IP or CIDR", part)
		}
		r.trusted = append(r.trusted, netip.PrefixFrom(a, a.BitLen()))
	}
	return r, nil
}

func (r *Resolver) isTrusted(a netip.Addr) bool {
	if r == nil {
		return false
	}
	a = a.Unmap()
	for _, p := range r.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func peer(req *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	return a.Unmap(), err == nil
}

// IP returns the client address. When the direct peer is a trusted proxy it
// walks X-Forwarded-For from the right and returns the first address that is
// not itself a trusted proxy. Right-to-left matters: a client can prepend
// entries but cannot change the ones the proxies append.
func (r *Resolver) IP(req *http.Request) string {
	p, ok := peer(req)
	if !ok {
		return req.RemoteAddr
	}
	if !r.isTrusted(p) {
		return p.String()
	}
	hops := strings.Split(strings.Join(req.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return p.String() // malformed header: do not guess
		}
		if !r.isTrusted(a) {
			return a.Unmap().String()
		}
	}
	return p.String()
}

// Secure reports whether the client reached the proxy over HTTPS. The
// X-Forwarded-Proto header counts only from a trusted proxy.
func (r *Resolver) Secure(req *http.Request) bool {
	if req.TLS != nil {
		return true
	}
	p, ok := peer(req)
	return ok && r.isTrusted(p) && strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https")
}
