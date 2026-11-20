// Package portcheck asks a service on the internet whether this server's web
// ports are reachable, and explains how to open them at the server's cloud
// provider when they aren't.
package portcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// serviceURL runs echoip (https://github.com/mpolden/echoip): GET
// /port/<n> dials the caller's address on port n and answers whether it
// connected.
const serviceURL = "https://ifconfig.co"

const (
	requestTimeout = 10 * time.Second
	maxAnswerSize  = 4 << 10
	// maxRetryWait caps how long a rate-limited check waits before its one
	// retry, so init is never held up for long.
	maxRetryWait = 20 * time.Second
)

// errRateLimited is a 429 answer; retryAfter is how long the service asked
// to wait.
type errRateLimited struct{ retryAfter time.Duration }

func (e errRateLimited) Error() string { return "port check is rate limited" }

// WebPorts are the ports Tero needs open: 80 for HTTP-01 (RFC 8555 §8.3) and
// the redirect to HTTPS, 443 for the dashboard, apps and TLS-ALPN-01 (RFC
// 8737).
var WebPorts = []int{80, 443}

// Status is what the connect-back service reported for one port.
type Status int

const (
	// Unchecked means there is no trustworthy answer.
	Unchecked Status = iota
	Open
	Closed
)

// Result is the status of one port.
type Result struct {
	Port   int
	Status Status
}

// Prober asks the connect-back service at BaseURL about ports on PublicIP.
type Prober struct {
	BaseURL  string
	PublicIP netip.Addr
	Client   *http.Client
}

// NewProber returns a Prober for the real service, over IPv4 only: the
// dashboard and app addresses are built on the server's IPv4 address.
func NewProber(publicIP netip.Addr) Prober {
	baseURL := serviceURL
	if override, isSet := testOverride(); isSet {
		baseURL = override
	}

	return Prober{BaseURL: baseURL, PublicIP: publicIP, Client: ipv4Client()}
}

func ipv4Client() *http.Client {
	dialer := &net.Dialer{Timeout: requestTimeout}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		// A redirect means the service changed; its answer can't be read.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Check asks about one port after the other, since ifconfig.co asks
// automated clients to go slowly. A port whose answer can't be trusted is
// Unchecked, never Open or Closed.
func (p Prober) Check(ctx context.Context, ports []int) []Result {
	results := make([]Result, len(ports))
	for index, port := range ports {
		results[index] = Result{Port: port, Status: p.checkWithRetry(ctx, port)}
	}
	return results
}

// checkWithRetry retries once when the service rate limits the check and
// asks for a short enough wait.
func (p Prober) checkWithRetry(ctx context.Context, port int) Status {
	status, err := p.checkPort(ctx, port)

	var limited errRateLimited
	isWorthRetrying := errors.As(err, &limited) && limited.retryAfter <= maxRetryWait
	if !isWorthRetrying {
		return status
	}

	select {
	case <-ctx.Done():
		return Unchecked
	case <-time.After(limited.retryAfter):
	}

	status, _ = p.checkPort(ctx, port)
	return status
}

// answer is echoip's port response.
type answer struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Reachable bool   `json:"reachable"`
}

func (p Prober) checkPort(ctx context.Context, port int) (Status, error) {
	url := strings.TrimSuffix(p.BaseURL, "/") + "/port/" + strconv.Itoa(port)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Unchecked, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "tero")

	response, err := p.Client.Do(request)
	if err != nil {
		return Unchecked, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		return Unchecked, errRateLimited{retryAfter: parseRetryAfter(response.Header.Get("Retry-After"))}
	}
	if response.StatusCode != http.StatusOK {
		return Unchecked, fmt.Errorf("port check answered %s", response.Status)
	}

	var got answer
	if err := json.NewDecoder(io.LimitReader(response.Body, maxAnswerSize)).Decode(&got); err != nil {
		return Unchecked, fmt.Errorf("read port check answer: %w", err)
	}

	return p.statusFrom(got, port)
}

// parseRetryAfter reads Retry-After in seconds (RFC 9110 §10.2.3). Without a
// usable value it waits a few seconds, which is enough for a burst limit.
func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 0 {
		return 5 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// statusFrom trusts an answer only when it is about this server's public
// address and the port asked. Outbound traffic that leaves through another
// address, such as a NAT gateway, says nothing about inbound reachability.
func (p Prober) statusFrom(got answer, port int) (Status, error) {
	answeredIP, err := netip.ParseAddr(got.IP)
	if err != nil {
		return Unchecked, fmt.Errorf("port check answered with %q, not an address", got.IP)
	}

	isAboutThisServer := answeredIP.Unmap() == p.PublicIP.Unmap()
	if !isAboutThisServer {
		return Unchecked, errors.New("port check saw another address than the server's public IP")
	}
	if got.Port != port {
		return Unchecked, fmt.Errorf("port check answered for port %d, not %d", got.Port, port)
	}

	if got.Reachable {
		return Open, nil
	}
	return Closed, nil
}
