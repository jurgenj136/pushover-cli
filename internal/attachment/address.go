package attachment

import (
	"net/netip"
	"strings"
)

var blockedIPv4 = prefixes(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
)

var blockedIPv6 = prefixes(
	"::/96",
	"::ffff:0:0/96",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"100:0:0:1::/64",
	"2001::/23",
	"2002::/16",
	"3fff::/20",
	"5f00::/16",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

func prefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}

// IsPublicAddress is deliberately conservative. It accepts only ordinary
// globally-routable unicast addresses and rejects the IANA special-purpose
// ranges listed in the approved remote-attachment policy.
func IsPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" || address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	if address.Is4In6() {
		return false
	}
	if address.Is4() {
		return !hasPrefix(address, blockedIPv4)
	}
	if !address.Is6() || strings.TrimSpace(address.String()) == "" {
		return false
	}
	return !hasPrefix(address, blockedIPv6)
}

func hasPrefix(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
