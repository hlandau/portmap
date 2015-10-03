// +build windows

package gateway

// Adapted from go:net/interface_windows.go
// TODO: IPv6
import "net"
import "syscall"
import "unsafe"
import "os"

func getAdapterList() (*syscall.IpAdapterInfo, error) {
	b := make([]byte, 1000)
	l := uint32(len(b))
	a := (*syscall.IpAdapterInfo)(unsafe.Pointer(&b[0]))
	// TODO(mikio): GetAdaptersInfo returns IP_ADAPTER_INFO that
	// contains IPv4 address list only. We should use another API
	// for fetching IPv6 stuff from the kernel.
	err := syscall.GetAdaptersInfo(a, &l)
	if err == syscall.ERROR_BUFFER_OVERFLOW {
		b = make([]byte, l)
		a = (*syscall.IpAdapterInfo)(unsafe.Pointer(&b[0]))
		err = syscall.GetAdaptersInfo(a, &l)
	}
	if err != nil {
		return nil, os.NewSyscallError("GetAdaptersInfo", err)
	}
	return a, nil
}

func getGatewayAddrs() (gwaddr []net.IP, err error) {
	ai, err := getAdapterList()
	if err != nil {
		return
	}

	for ; ai != nil; ai = ai.Next {
		g := &ai.GatewayList
		for ; g != nil; g = g.Next {
			s := string(g.IpAddress.String[:])
			ip := net.ParseIP(s)
			gwaddr = append(gwaddr, ip)
		}
	}

	return
}
