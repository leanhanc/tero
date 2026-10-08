package initflow

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// waitForCertificate reports whether the local Tero service presents a
// certificate for domain before the timeout. It connects to 127.0.0.1 so it
// works before DNS has propagated.
func waitForCertificate(ctx context.Context, domain string, timeout time.Duration) bool {
	return waitFor(ctx, timeout, func() bool { return servesCertificateFor(domain) })
}

func servesCertificateFor(domain string) bool {
	dialer := &net.Dialer{Timeout: 3 * time.Second}

	// Only the presence of a certificate for the name is checked here; the
	// chain is verified by every real client.
	connection, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
	})
	if err != nil {
		return false
	}
	defer connection.Close()

	peerCertificates := connection.ConnectionState().PeerCertificates
	if len(peerCertificates) == 0 {
		return false
	}

	return peerCertificates[0].VerifyHostname(domain) == nil
}
