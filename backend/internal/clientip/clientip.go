// Package clientip finds the real public IP of a request. The office network
// check depends on it, so it never trusts a header the browser could set:
// X-Forwarded-For is only read when the direct peer is one of our proxies,
// and then from the right, skipping our own proxies.
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type Resolver struct {
	Trusted []netip.Prefix
}

// Parse reads a comma-separated list of CIDRs or single IPs.
func Parse(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			a, err := netip.ParseAddr(part)
			if err != nil {
				return nil, err
			}
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func (r Resolver) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range r.Trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IP returns the client address for the request.
func (r Resolver) IP(req *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	peer = peer.Unmap()
	if !r.trusted(peer) {
		return peer
	}
	var hops []string
	for _, h := range req.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// A garbled entry: stop at the last address we could trust.
			return peer
		}
		a = a.Unmap()
		if !r.trusted(a) {
			return a
		}
		peer = a
	}
	return peer
}
