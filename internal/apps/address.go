package apps

// Host is an app's address without the scheme.
func Host(name, publicIP string) string {
	return name + "." + Domain(publicIP)
}

// Domain is what every app's address ends in: <public-ip>.nip.io whatever
// the dashboard domain is.
func Domain(publicIP string) string {
	if domain, isSet := testDomain(); isSet {
		return domain
	}

	return publicIP + ".nip.io"
}
