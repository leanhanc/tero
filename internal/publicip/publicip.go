// Package publicip finds the server's public IPv4 address, used to build the
// default <ip>.sslip.io dashboard domain.
package publicip

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// traceURL answers with the caller's address as seen from the internet. It is
// only asked when no network interface has a public address, which is the
// case on clouds that NAT their instances (EC2, GCP).
const traceURL = "https://1.1.1.1/cdn-cgi/trace"

// Detect returns the server's public IPv4 address.
func Detect(ctx context.Context) (netip.Addr, error) {
	if override, isSet := testOverride(); isSet {
		return override, nil
	}

	interfaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("list network interfaces: %w", err)
	}

	if addr, isFound := FirstPublic(interfaceAddrs); isFound {
		return addr, nil
	}

	return askInternet(ctx)
}

// FirstPublic returns the first globally routable IPv4 address in addrs.
func FirstPublic(addrs []net.Addr) (netip.Addr, bool) {
	for _, candidate := range addrs {
		prefix, err := netip.ParsePrefix(candidate.String())
		if err != nil {
			continue
		}

		addr := prefix.Addr()
		isPublicIPv4 := addr.Is4() && addr.IsGlobalUnicast() && !addr.IsPrivate() && !isSharedAddressSpace(addr)
		if isPublicIPv4 {
			return addr, true
		}
	}

	return netip.Addr{}, false
}

// isSharedAddressSpace matches 100.64.0.0/10 (RFC 6598, carrier-grade NAT),
// which netip does not treat as private.
func isSharedAddressSpace(addr netip.Addr) bool {
	return netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
}

func askInternet(ctx context.Context) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
	if err != nil {
		return netip.Addr{}, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("look up public IP: %w", err)
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		value, isIPLine := strings.CutPrefix(scanner.Text(), "ip=")
		if !isIPLine {
			continue
		}

		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is4() {
			break
		}
		return addr, nil
	}

	return netip.Addr{}, fmt.Errorf("look up public IP: no IPv4 address in the answer from %s", traceURL)
}
