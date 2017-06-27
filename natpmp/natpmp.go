// Package natpmp provides low level NAT-PMP protocol implementation functions.
package natpmp

import gnet "net"
import "errors"
import "fmt"
import "time"
import "github.com/hlandau/degoutils/net"
import "bytes"
import "encoding/binary"

// Opcodes
type opcodeNo byte

const (
	opcGetExternalAddr opcodeNo = iota
	opcMapUDP                   = 1
	opcMapTCP                   = 2
)

type Protocol int

const (
	TCP Protocol = 6  // Map TCP port.
	UDP          = 17 // Map UDP port.
)

func (p Protocol) opcode() (opcodeNo, bool) {
	switch p {
	case TCP:
		return opcMapTCP, true
	case UDP:
		return opcMapUDP, true
	default:
		return 0, false
	}
}

// Port which listens on the gateway.
const hostToGatewayPort = 5351
const version0 byte = 0

var backoff = net.Backoff{
	MaxTries:           9,
	InitialDelay:       250 * time.Millisecond,
	MaxDelay:           64000 * time.Millisecond, // InitialDelay*8
	MaxDelayAfterTries: 8,
}

var natpmpErrTimeout = errors.New("Request timed out.")

func makeRequest(dst gnet.IP, opcode opcodeNo, data []byte) ([]byte, error) {
	conn, err := gnet.DialUDP("udp", nil, &gnet.UDPAddr{dst, hostToGatewayPort, ""})
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	msg := make([]byte, 2)
	msg[0] = version0     // Version 0
	msg[1] = byte(opcode) // Opcode
	msg = append(msg, data...)

	rconf := backoff
	rconf.Reset()

	for {
		// here we use the 'delay' as the timeout
		maxtime := rconf.NextDelay()
		if maxtime == 0 {
			// max tries reached
			break
		}

		err = conn.SetDeadline(time.Now().Add(maxtime))
		if err != nil {
			return nil, err
		}

		_, err = conn.Write(msg)
		if err != nil {
			return nil, err
		}

		var res []byte
		var uaddr *gnet.UDPAddr
		res, uaddr, err = net.ReadDatagramFromUDP(conn)
		if err != nil {
			if err.(gnet.Error).Timeout() {
				// try again
				continue
			}
			return nil, err
		}

		if !uaddr.IP.Equal(dst) || uaddr.Port != hostToGatewayPort {
			continue
		}

		if len(res) < 4 {
			continue
		}

		if res[0] != 0 || res[1] != (0x80|byte(opcode)) {
			continue
		}

		rc := binary.BigEndian.Uint16(res[2:])

		if rc != 0 {
			return nil, fmt.Errorf("NAT-PMP: default gateway responded with nonzero error code %d", rc)
		}

		return res[4:], nil
	}

	return nil, natpmpErrTimeout
}

// Performs a NAT-PMP transaction to get the external address.
func GetExternalAddr(gwaddr gnet.IP) (gnet.IP, error) {
	r, err := makeRequest(gwaddr, opcGetExternalAddr, []byte{})
	if err != nil {
		return nil, err
	}

	//time = r[0:4]
	return r[4:8], nil
}

// Performs a single Map Port NAT-PMP transaction. This is a low-level function
// as it does not manage the renewal of the mapping when it expires.
//
// If suggestedExternalPort is 0, any available port will be chosen.
func Map(gwaddr gnet.IP, proto Protocol,
	internalPort, suggestedExternalPort uint16,
	lifetime time.Duration) (externalPort uint16, actualLifetime time.Duration, err error) {

	opc, ok := proto.opcode()
	if !ok {
		err = errors.New("unsupported protocol")
		return
	}

	b := bytes.NewBuffer(make([]byte, 0, 10))
	binary.Write(b, binary.BigEndian, struct {
		Reserved                            uint16
		InternalPort, SuggestedExternalPort uint16
		Lifetime                            uint32
	}{0, internalPort, suggestedExternalPort, uint32(lifetime.Seconds())})

	r, err := makeRequest(gwaddr, opc, b.Bytes())
	if err != nil {
		return
	}

	if len(r) < 12 {
		err = errors.New("short response")
		return
	}

	// r[0: 4] // time
	// r[4: 6] // internal port
	// r[6: 8] // mapped external port
	// r[8:12] // lifetime
	externalPort = binary.BigEndian.Uint16(r[6:8])
	actualLifetime = time.Duration(binary.BigEndian.Uint32(r[8:12])) * time.Second
	return
}
