package gateway

import "net"

// Get the IPs of default gateways for this host.
//
// Both IPv4 and IPv6 default gateways are returned and each protocol may have
// more than one default gateway.
func GetIPs() ([]net.IP, error) {
	return getGatewayAddrs()
}
