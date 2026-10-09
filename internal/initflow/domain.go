package initflow

import (
	"bufio"
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/leanhanc/tero/internal/publicip"
)

// resolveDomain picks the dashboard domain: --domain if given, otherwise the
// admin's answer at the prompt, otherwise <public-ip>.sslip.io.
func resolveDomain(ctx context.Context, opts Options) (string, error) {
	if opts.Domain != "" {
		return normalizeDomain(opts.Domain)
	}

	ip, err := publicip.Detect(ctx)
	if err != nil {
		return "", fmt.Errorf("%w\nPass a domain instead: sudo tero init --domain dashboard.example.com", err)
	}

	return resolveDomainWithIP(opts, ip)
}

// resolveDomainWithIP asks for a domain when attached to a terminal and
// falls back to the server's sslip.io name.
func resolveDomainWithIP(opts Options, ip netip.Addr) (string, error) {
	fallback := sslipDomain(ip)

	if !opts.IsInteractive {
		return fallback, nil
	}

	fmt.Fprintf(opts.Out, "If you have a domain for the dashboard, type it. Otherwise press Enter to use %s: ", fallback)
	answer, err := bufio.NewReader(opts.In).ReadString('\n')
	if err != nil && answer == "" {
		return fallback, nil
	}

	trimmedAnswer := strings.TrimSpace(answer)
	if trimmedAnswer == "" {
		return fallback, nil
	}

	return normalizeDomain(trimmedAnswer)
}

func sslipDomain(ip netip.Addr) string {
	return ip.String() + ".sslip.io"
}

// normalizeDomain lowercases a domain and checks it is a hostname Let's
// Encrypt can issue for.
func normalizeDomain(raw string) (string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")

	if _, err := netip.ParseAddr(domain); err == nil {
		return "", fmt.Errorf("%q is an IP address; the dashboard needs a domain name (or leave it empty to use %s.sslip.io)", raw, domain)
	}

	labels := strings.Split(domain, ".")
	if len(labels) < 2 || len(domain) > 253 {
		return "", fmt.Errorf("%q is not a valid domain name, e.g. dashboard.example.com", raw)
	}

	for _, label := range labels {
		if !isValidLabel(label) {
			return "", fmt.Errorf("%q is not a valid domain name, e.g. dashboard.example.com", raw)
		}
	}

	// No top-level domain is all digits; a name like 1.2.3 can't get a
	// certificate.
	topLevel := labels[len(labels)-1]
	if strings.Trim(topLevel, "0123456789") == "" {
		return "", fmt.Errorf("%q is not a valid domain name, e.g. dashboard.example.com", raw)
	}

	return domain, nil
}

func isValidLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}

	for _, char := range label {
		isAllowed := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-'
		if !isAllowed {
			return false
		}
	}

	return true
}
