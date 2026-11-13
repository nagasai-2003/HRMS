package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestIP(t *testing.T) {
	trusted, err := Parse("127.0.0.1, 10.0.0.0/8, ::1")
	if err != nil {
		t.Fatal(err)
	}
	r := Resolver{Trusted: trusted}
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct client, header ignored", "49.205.112.18:5000", "1.2.3.4", "49.205.112.18"},
		{"through our proxy", "127.0.0.1:5000", "49.205.112.18", "49.205.112.18"},
		{"spoofed left entry is ignored", "127.0.0.1:5000", "1.2.3.4, 49.205.112.18", "49.205.112.18"},
		{"two of our proxies", "10.0.0.5:80", "49.205.112.18, 10.0.0.9", "49.205.112.18"},
		{"proxy without header", "127.0.0.1:5000", "", "127.0.0.1"},
		{"IPv6 loopback proxy", "[::1]:5000", "183.82.96.7", "183.82.96.7"},
		{"garbled header", "127.0.0.1:5000", "nonsense", "127.0.0.1"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.remote
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := r.IP(req).String(); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}
