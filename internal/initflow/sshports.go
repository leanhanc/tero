package initflow

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
)

// sshPorts returns every TCP port the SSH server listens on, so the firewall
// keeps them open. sshd's own settings (Port, ListenAddress) and, on releases
// that start sshd through socket activation, ssh.socket's listeners are both
// read, since the socket's ports win when it is in use.
func sshPorts(ctx context.Context, h host) ([]int, error) {
	effectiveConfig, err := h.run.Run(ctx, "sshd", "-T")
	if err != nil {
		return nil, fmt.Errorf("read the SSH server's settings: %w", err)
	}

	ports := portsFromSSHDConfig(effectiveConfig)

	socketListeners, err := h.run.Run(ctx, "systemctl", "show", "ssh.socket", "--property=Listen")
	if err == nil {
		ports = append(ports, portsFromSocketListeners(socketListeners)...)
	}

	slices.Sort(ports)
	ports = slices.Compact(ports)
	if len(ports) == 0 {
		return nil, fmt.Errorf("couldn't find the port the SSH server listens on")
	}

	return ports, nil
}

// portsFromSSHDConfig reads "port 22" and "listenaddress 0.0.0.0:22" lines.
func portsFromSSHDConfig(effectiveConfig string) []int {
	var ports []int
	for _, line := range strings.Split(effectiveConfig, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch key {
		case "port":
			ports = appendPort(ports, value)
		case "listenaddress":
			_, port, err := net.SplitHostPort(value)
			if err == nil {
				ports = appendPort(ports, port)
			}
		}
	}

	return ports
}

// portsFromSocketListeners reads systemctl's "Listen=[::]:22 (Stream)" lines.
func portsFromSocketListeners(output string) []int {
	var ports []int
	for _, line := range strings.Split(output, "\n") {
		value, isListenLine := strings.CutPrefix(strings.TrimSpace(line), "Listen=")
		address, isStream := strings.CutSuffix(value, " (Stream)")
		if !isListenLine || !isStream {
			continue
		}

		if _, port, err := net.SplitHostPort(address); err == nil {
			ports = appendPort(ports, port)
			continue
		}
		ports = appendPort(ports, address)
	}

	return ports
}

func appendPort(ports []int, value string) []int {
	port, err := strconv.Atoi(value)
	isValidPort := err == nil && port > 0 && port < 65536
	if !isValidPort {
		return ports
	}

	return append(ports, port)
}

// firewallRules fills the SSH ports into the embedded ruleset.
func firewallRules(ports []int) ([]byte, error) {
	template, err := embedded.ReadFile("files/firewall.nft")
	if err != nil {
		return nil, err
	}

	openPorts := make([]string, 0, len(ports)+2)
	for _, port := range ports {
		openPorts = append(openPorts, strconv.Itoa(port))
	}
	openPorts = append(openPorts, "80", "443")
	openPorts = slices.Compact(openPorts)

	rules := strings.Replace(string(template), "@TCP_PORTS@", strings.Join(openPorts, ", "), 1)
	return []byte(rules), nil
}
