// +build !linux,!windows

package gateway

import "net"
import "errors"

var errNotSupportedGw = errors.New("GetGatewayAddrs is not supported on this platform")

func getGatewayAddrs() (gwaddr []net.IP, err error) {
	return nil, errNotSupportedGw
}
