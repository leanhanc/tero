package portcheck

import (
	"fmt"
	"strings"
)

// Everything init prints about ports is here, so the wording can change
// without touching the logic.

// docsURL is the public page with every provider's steps.
const docsURL = "https://github.com/leanhanc/tero/blob/main/docs/ports.md"

func openLine(port int) string {
	return fmt.Sprintf("Port %d is open to the internet.", port)
}

func closedLine(port int) string {
	return fmt.Sprintf("Port %d is closed to the internet. %s", port, portNeed(port))
}

func uncheckedLine(port int) string {
	return fmt.Sprintf("Couldn't check whether port %d is open to the internet.", port)
}

func portNeed(port int) string {
	if port == 443 {
		return "The dashboard, your apps and HTTPS certificates need it."
	}
	return "The redirect to HTTPS and one of the two ways to get HTTPS certificates use it."
}

func moreHelpLine() string {
	return "Steps for every provider: " + docsURL
}

// providerHelp is the step-by-step help to open the closed ports.
func providerHelp(provider Provider, closed []int) []string {
	ports := joinPorts(closed)

	switch provider {
	case AWS:
		return []string{
			fmt.Sprintf("To open %s on AWS, add inbound rules to this server's security group:", ports),
			"  1. In the EC2 console, open Instances, select this server and open the Security tab.",
			"  2. Click its security group, then Edit inbound rules.",
			fmt.Sprintf("  3. For each port, click Add rule: type %s, source Anywhere-IPv4.", awsRuleTypes(closed)),
			"  4. Click Save rules.",
			"On Lightsail, add the rules on the instance's Networking tab instead.",
		}
	case GoogleCloud:
		return []string{
			fmt.Sprintf("To open %s on Google Cloud, allow them in the VPC firewall:", ports),
			"  1. In the Compute Engine console, open VM instances, click this server, then Edit.",
			fmt.Sprintf("  2. Under Firewalls, tick %s.", gcpCheckboxes(closed)),
			"  3. Click Save.",
		}
	case Azure:
		return []string{
			fmt.Sprintf("To open %s on Azure, add an inbound rule to the network security group:", ports),
			"  1. In the Azure portal, open this virtual machine, then Networking > Network settings.",
			"  2. Click Create port rule > Inbound port rule.",
			fmt.Sprintf("  3. Destination port ranges: %s, protocol TCP, action Allow.", commaPorts(closed)),
			"  4. Click Add.",
		}
	case OracleCloud:
		return []string{
			fmt.Sprintf("To open %s on Oracle Cloud, add ingress rules to the subnet's security list:", ports),
			"  1. In the OCI console, open this instance and click its subnet.",
			"  2. Under Security, open the security list the subnet uses (by default, Default Security List for <your VCN>).",
			"  3. For each port, click Add Ingress Rules: source CIDR 0.0.0.0/0, IP protocol TCP, destination port range the port.",
			"  4. Click Add Ingress Rules.",
			"Oracle's Ubuntu images also block these ports in the server's own iptables rules. The page below shows how to open them.",
		}
	default:
		return []string{
			fmt.Sprintf("Open TCP %s to all addresses (0.0.0.0/0) in your provider's firewall.", ports),
			"A firewall on the server itself, such as ufw, can also block them.",
		}
	}
}

func joinPorts(ports []int) string {
	labels := make([]string, len(ports))
	for index, port := range ports {
		labels[index] = fmt.Sprint(port)
	}

	noun := "port"
	if len(ports) > 1 {
		noun = "ports"
	}
	return noun + " " + strings.Join(labels, " and ")
}

func commaPorts(ports []int) string {
	labels := make([]string, len(ports))
	for index, port := range ports {
		labels[index] = fmt.Sprint(port)
	}
	return strings.Join(labels, ",")
}

// webName is the protocol name consoles use for a web port.
func webName(port int) string {
	if port == 443 {
		return "HTTPS"
	}
	return "HTTP"
}

func awsRuleTypes(ports []int) string {
	names := make([]string, len(ports))
	for index, port := range ports {
		names[index] = webName(port)
	}
	return strings.Join(names, " or ")
}

func gcpCheckboxes(ports []int) string {
	names := make([]string, len(ports))
	for index, port := range ports {
		names[index] = "Allow " + webName(port) + " traffic"
	}
	return strings.Join(names, " and ")
}
