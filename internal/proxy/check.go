package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// Status represents the result of a proxy health check.
type Status struct {
	URL       string `json:"url"`
	Alive     bool   `json:"alive"`
	Latency   int64  `json:"latency"`    // milliseconds
	Error     string `json:"error"`
	Protocol  string `json:"protocol"`   // detected: socks5, http, https
	Since     int64  `json:"since"`      // unix timestamp when proxy went alive
	BytesSent int64  `json:"bytes_sent"` // accumulated bytes sent through this proxy
	BytesRecv int64  `json:"bytes_recv"` // accumulated bytes received through this proxy
}

// CheckHealth tests a proxy by its protocol (HTTP, HTTPS, SOCKS5).
// If no scheme is given, auto-detect by trying SOCKS5 → HTTP → HTTPS.
func CheckHealth(proxyUrl string) Status {
	raw := strings.TrimSpace(proxyUrl)

	// Convert legacy 4-part format host:port:user:pass → user:pass@host:port
	if !strings.Contains(raw, "://") && !strings.Contains(raw, "@") {
		parts := strings.Split(raw, ":")
		if len(parts) == 4 {
			raw = fmt.Sprintf("%s:%s@%s:%s", parts[2], parts[3], parts[0], parts[1])
		}
	}

	hasScheme := strings.Contains(raw, "://")

	if hasScheme {
		u, err := url.Parse(raw)
		if err != nil {
			return Status{URL: proxyUrl, Error: fmt.Sprintf("invalid URL: %v", err)}
		}
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "http", "https":
			return checkHTTPProxy(proxyUrl, raw, scheme)
		default:
			return checkSOCKS5Proxy(proxyUrl, u)
		}
	}

	// No scheme — auto-detect: try socks5, http, https IN PARALLEL
	tempURL := "socks5://" + raw
	u, err := url.Parse(tempURL)
	if err != nil {
		return Status{URL: proxyUrl, Error: fmt.Sprintf("invalid URL: %v", err)}
	}
	hostWithAuth := u.Host
	if u.User != nil {
		hostWithAuth = u.User.String() + "@" + u.Host
	}

	type detectResult struct {
		status   Status
		priority int // lower = preferred (0=socks5, 1=http, 2=https)
	}
	ch := make(chan detectResult, 3)
	go func() { ch <- detectResult{checkSOCKS5Proxy(proxyUrl, u), 0} }()
	go func() { ch <- detectResult{checkHTTPProxy(proxyUrl, "http://"+hostWithAuth, "http"), 1} }()
	go func() { ch <- detectResult{checkHTTPProxy(proxyUrl, "https://"+hostWithAuth, "https"), 2} }()

	var best *detectResult
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.status.Alive {
			if best == nil || r.priority < best.priority {
				best = &r
			}
		}
	}
	if best != nil {
		return best.status
	}

	return Status{URL: proxyUrl, Error: "all protocols failed (socks5/http/https)"}
}

// checkHTTPProxy tests an HTTP/HTTPS proxy by making a request through it.
func checkHTTPProxy(originalUrl, normalized, protocol string) Status {
	result := Status{URL: originalUrl, Protocol: protocol}

	proxyURL, err := url.Parse(normalized)
	if err != nil {
		result.Error = fmt.Sprintf("invalid URL: %v", err)
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://www.gstatic.com/generate_204", nil)
	if err != nil {
		result.Error = fmt.Sprintf("request error: %v", err)
		return result
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		result.Latency = elapsed
		result.Error = fmt.Sprintf("connect failed: %v", err)
		return result
	}
	resp.Body.Close()

	result.Alive = resp.StatusCode >= 200 && resp.StatusCode < 500
	result.Latency = elapsed
	if !result.Alive {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

// checkSOCKS5Proxy tests a SOCKS5 proxy by dialing through it.
func checkSOCKS5Proxy(originalUrl string, u *url.URL) Status {
	result := Status{URL: originalUrl, Protocol: "socks5"}

	var auth *proxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &proxy.Auth{
			User:     u.User.Username(),
			Password: pass,
		}
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host = host + ":1080"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	netDialer := &net.Dialer{Timeout: 5 * time.Second}
	dialer, err := proxy.SOCKS5("tcp", host, auth, netDialer)
	if err != nil {
		result.Error = fmt.Sprintf("dialer error: %v", err)
		return result
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	start := time.Now()
	go func() {
		conn, err := dialer.Dial("tcp", "google.com:80")
		ch <- dialResult{conn, err}
	}()

	select {
	case <-ctx.Done():
		elapsed := time.Since(start).Milliseconds()
		result.Latency = elapsed
		result.Error = "timeout after 5s"
		// Clean up the goroutine's connection when it eventually completes
		go func() {
			if dr := <-ch; dr.conn != nil {
				dr.conn.Close()
			}
		}()
		return result
	case dr := <-ch:
		elapsed := time.Since(start).Milliseconds()
		if dr.err != nil {
			result.Latency = elapsed
			result.Error = fmt.Sprintf("connect failed: %v", dr.err)
			return result
		}
		dr.conn.Close()
		result.Alive = true
		result.Latency = elapsed
		return result
	}
}

// BuildProxyURL constructs a full proxy URL using the detected protocol.
func BuildProxyURL(raw, protocol string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		return raw
	}
	if protocol == "" {
		protocol = "socks5"
	}

	if strings.Contains(raw, "@") {
		return protocol + "://" + raw
	}

	parts := strings.Split(raw, ":")
	if len(parts) == 4 {
		return fmt.Sprintf("%s://%s:%s@%s:%s", protocol, parts[2], parts[3], parts[0], parts[1])
	}

	return protocol + "://" + raw
}

// NormalizeURL accepts various proxy formats and returns a trimmed URL.
func NormalizeURL(raw string) string {
	return strings.TrimSpace(raw)
}
