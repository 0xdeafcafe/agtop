package plugin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Proxy is one plugin's way out: an HTTP CONNECT proxy on a localhost port
// of its own, the only address its sandbox may connect to. It tunnels to the
// hosts the plugin was approved for and nowhere else, and never to a
// loopback, private or link-local address, whatever a name resolves to.
type Proxy struct {
	ln    net.Listener
	allow map[string]bool // host:port
	log   *log.Logger

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// Listen starts a proxy for the given host:port entries.
func Listen(network []string, lg *log.Logger) (*Proxy, error) {
	allow := map[string]bool{}
	for _, n := range network {
		h, p, err := SplitHostPort(n)
		if err != nil {
			return nil, err
		}
		allow[net.JoinHostPort(h, strconv.Itoa(p))] = true
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if lg == nil {
		lg = log.New(io.Discard, "", 0)
	}
	px := &Proxy{ln: ln, allow: allow, log: lg, conns: map[net.Conn]struct{}{}}
	go px.serve()
	return px, nil
}

// Port is the port it listens on.
func (p *Proxy) Port() int { return p.ln.Addr().(*net.TCPAddr).Port }

// Close stops it and cuts every tunnel.
func (p *Proxy) Close() error {
	err := p.ln.Close()
	p.mu.Lock()
	for c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	return err
}

func (p *Proxy) track(c net.Conn, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if on {
		p.conns[c] = struct{}{}
	} else {
		delete(p.conns, c)
	}
}

func (p *Proxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *Proxy) handle(c net.Conn) {
	p.track(c, true)
	defer p.track(c, false)
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		fmt.Fprint(c, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	}
	target := req.Host
	h, port, err := SplitHostPort(target)
	if err != nil || !p.allow[net.JoinHostPort(h, strconv.Itoa(port))] {
		p.log.Printf("proxy: refused %s: not approved", target)
		fmt.Fprint(c, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	}
	up, err := dialPublic(h, port)
	if err != nil {
		p.log.Printf("proxy: %s: %v", target, err)
		fmt.Fprint(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	}
	p.track(up, true)
	defer p.track(up, false)
	defer up.Close()
	_ = c.SetReadDeadline(time.Time{})
	fmt.Fprint(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	go func() {
		// Anything the client sent after its request is already buffered.
		_, _ = io.Copy(up, br)
		if tc, ok := up.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, up)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// dialPublic connects to host:port, refusing any address that isn't on the
// public internet, so an approved name can't be pointed at your machine or
// your network.
func dialPublic(host string, port int) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error = fmt.Errorf("%s has no public address", host)
	for _, ip := range ips {
		if !public(ip.IP) {
			continue
		}
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), strconv.Itoa(port)))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

func public(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() &&
		!cgnat.Contains(ip)
}

// cgnat is carrier-grade NAT space, which is private in practice (and where
// Tailscale puts your machines).
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
