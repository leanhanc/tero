package publicip

import (
	"net"
	"testing"
)

func cidr(t *testing.T, value string) net.Addr {
	t.Helper()
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		t.Fatal(err)
	}
	network.IP = ip
	return network
}

func TestFirstPublic(t *testing.T) {
	addrs := []net.Addr{
		cidr(t, "127.0.0.1/8"),
		cidr(t, "10.0.2.15/24"),
		cidr(t, "100.64.1.2/10"),
		cidr(t, "2001:db8::1/64"),
		cidr(t, "203.0.113.10/24"),
	}

	got, isFound := FirstPublic(addrs)
	if !isFound || got.String() != "203.0.113.10" {
		t.Fatalf("FirstPublic() = %v, %v", got, isFound)
	}
}

func TestFirstPublicNone(t *testing.T) {
	addrs := []net.Addr{cidr(t, "192.168.64.5/24"), cidr(t, "172.31.4.4/20")}

	if _, isFound := FirstPublic(addrs); isFound {
		t.Fatal("found a public address among private ones")
	}
}
