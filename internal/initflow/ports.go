package initflow

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/leanhanc/tero/internal/portcheck"
)

// checkWebPorts asks the internet whether it can reach Tero's web ports and
// tells the admin how to open the closed ones. A port Tero itself isn't
// listening on can't be checked, since the answer would blame the provider.
func checkWebPorts(ctx context.Context, h host, publicIP netip.Addr, style portcheck.Style) []portcheck.Result {
	isListening := waitFor(ctx, 30*time.Second, isListeningOnWebPorts)

	results := uncheckedResults()
	if isListening {
		results = portcheck.NewProber(publicIP).Check(ctx, portcheck.WebPorts)
	}

	portcheck.Write(h.out, results, portcheck.DetectProvider(h.files), style)
	return results
}

func isListeningOnWebPorts() bool {
	for _, port := range portcheck.WebPorts {
		address := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port)).String()
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			return false
		}
		connection.Close()
	}
	return true
}

func uncheckedResults() []portcheck.Result {
	results := make([]portcheck.Result, len(portcheck.WebPorts))
	for index, port := range portcheck.WebPorts {
		results[index] = portcheck.Result{Port: port, Status: portcheck.Unchecked}
	}
	return results
}
