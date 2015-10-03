package portmap

import "net"
import "github.com/hlandau/portmap/gateway"
import "github.com/hlandau/portmap/natpmp"

// Attempt to obtain the external IP address from the default gateway.
//
// If the host has a globally routable IP, returns that IP.
//
// Currently this only tries NAT-PMP and does not attempt to learn the external
// IP address via UPnP.
//
// This function is not very useful because the IP address returned may still
// be an RFC1918 address, due to the possibility of a double NAT setup. There
// are better solutions for obtaining one's public IP address, such as STUN.
func ExternalAddr() (net.IP, error) {
	if gr, ip := isGloballyRoutable(); gr {
		return ip, nil
	}

	gwa, err := gateway.GetIPs()
	if err != nil {
		return nil, err
	}

	var extaddr net.IP
	for _, gw := range gwa {
		extaddr, err = natpmp.GetExternalAddr(gw)
		if err == nil {
			return extaddr, nil
		}
	}

	return nil, err
}
