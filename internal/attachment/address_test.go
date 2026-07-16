package attachment

import (
	"net/netip"
	"testing"
)

func TestIsPublicAddress(t *testing.T) {
	blocked := []string{
		"0.1.2.3", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1", "192.0.0.1", "192.0.2.1", "192.88.99.1", "192.168.1.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1",
		"::", "::1", "::ffff:8.8.8.8", "64:ff9b::808:808", "64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001::1", "2002::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "ff00::1",
	}
	for _, value := range blocked {
		t.Run(value, func(t *testing.T) {
			if IsPublicAddress(netip.MustParseAddr(value)) {
				t.Fatal("unsafe address accepted")
			}
		})
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !IsPublicAddress(netip.MustParseAddr(value)) {
			t.Fatalf("public address %s rejected", value)
		}
	}
}

func TestIsPublicAddressRejectsIPv6Zone(t *testing.T) {
	address := netip.MustParseAddr("2606:4700:4700::1111").WithZone("en0")
	if IsPublicAddress(address) {
		t.Fatal("zoned IPv6 address accepted")
	}
}
