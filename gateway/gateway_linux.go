// +build linux

package gateway

import "net"
import "syscall"

func getGatewayAddrs() (gwaddr []net.IP, err error) {
	rib, err := syscall.NetlinkRIB(syscall.RTM_GETROUTE, syscall.AF_INET)
	if err != nil {
		return
	}

	ribm, err := syscall.ParseNetlinkMessage(rib)
	if err != nil {
		return
	}

loop:
	for _, m := range ribm {
		switch m.Header.Type {
		case syscall.RTM_NEWROUTE:
			ra, err := syscall.ParseNetlinkRouteAttr(&m)
			if err != nil {
				continue
			}

			for _, a := range ra {
				switch a.Attr.Type {
				case syscall.RTA_GATEWAY:
					var ip net.IP = a.Value[0:4]
					gwaddr = append(gwaddr, ip.To16())

				default:
				}
			}

		case syscall.NLMSG_DONE:
			break loop
		}
	}

	return
}
