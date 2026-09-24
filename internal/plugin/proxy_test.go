package plugin

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
)

func connect(t *testing.T, px *Proxy, req string) int {
	t.Helper()
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(px.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, req)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestProxyTunnelsOnlyToApprovedPublicHosts(t *testing.T) {
	// A server on this machine, named by an approved host: the proxy must
	// still refuse it, as a name can be made to point anywhere.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	local := "localhost:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	px, err := Listen([]string{"api.example.com:443", local}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()

	cases := []struct {
		name, req string
		want      int
	}{
		{"unapproved host", "CONNECT evil.example.com:443 HTTP/1.1\r\nHost: evil.example.com:443\r\n\r\n", 403},
		{"approved host, other port", "CONNECT api.example.com:22 HTTP/1.1\r\nHost: api.example.com:22\r\n\r\n", 403},
		{"ip literal", "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\n\r\n", 403},
		{"approved name for this machine", "CONNECT " + local + " HTTP/1.1\r\nHost: " + local + "\r\n\r\n", 502},
		{"plain http", "GET http://api.example.com/ HTTP/1.1\r\nHost: api.example.com\r\n\r\n", 405},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := connect(t, px, c.req); got != c.want {
				t.Fatalf("status %d, want %d", got, c.want)
			}
		})
	}
}

func TestPublic(t *testing.T) {
	for ip, want := range map[string]bool{
		"1.1.1.1": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "192.168.1.1": false, "172.16.0.1": false,
		"169.254.169.254": false, "100.100.1.1": false, "::1": false, "fe80::1": false, "fd00::1": false, "0.0.0.0": false,
	} {
		if got := public(net.ParseIP(ip)); got != want {
			t.Errorf("public(%s) = %v", ip, got)
		}
	}
}
