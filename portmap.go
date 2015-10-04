// Package portmap provides a utility for the automatic mapping of TCP and UDP
// ports via NAT-PMP or UPnP IGDv1.
//
// In order to map a TCP or UDP port, just call New. Negotiation via NAT-PMP
// and, if that fails, via UPnP, will be attempted in the background.
//
// You can interrogate the returned Mapping object to determine when the
// mapping has been successfully created, and to cancel the mapping.
package portmap

import "net"
import "fmt"
import "time"
import "sync"
import "strconv"
import denet "github.com/hlandau/degoutils/net"
import "github.com/hlandau/portmap/gateway"
import "github.com/hlandau/portmap/ssdp"

// Identifies a transport layer protocol.
type Protocol int

const (
	TCP Protocol = 6  // Map a TCP port
	UDP          = 17 // Map a UDP port
)

// Specifies a port mapping which will be created.
type Config struct {
	// The protocol for which the port should be mapped.
	//
	// Must be TCP or UDP.
	Protocol Protocol

	// A short description for the mapping. This may not be used in all cases.
	//
	// If it is left blank, a name will be generated automatically.
	Name string

	// The internal port on this host to map to.
	//
	// Mapping to ports on other hosts is not supported.
	InternalPort uint16

	// The external port to be used. When passing MappingConfig to
	// CreatePortMapping, this is purely advisory. If you want portmap to choose
	// a port itself, set this to zero.
	//
	// In order to determine the external port actually allocated, call
	// GetConfig() on a Mapping object after it becomes active.
	// The allocated port number may be different to the originally specified
	// ExternalPort value, even if it was nonzero.
	ExternalPort uint16

	// The lifetime of the mapping in seconds. The mapping will automatically be
	// renewed halfway through a lifetime period, so this value determines how
	// long a mapping will stick around when the program exits, if the mapping is
	// not deleted beforehand.
	Lifetime time.Duration

	// Determines the backoff delays used between NAT-PMP or UPnP mapping
	// attempts. Note that if you set MaxTries to a nonzero value, the mapping
	// process will give up after that many tries.
	//
	// It is recommended that you use the nil value for this struct, which will
	// cause sensible defaults to be used with no limit on retries.
	Backoff denet.Backoff
}

// A mapping is active if its ExternalAddr() function returns a non-empty string.
//
// The value returned by ExternalAddr() may change over time. The mapping may
// go from inactive to active, active to inactive, or may change external addresses
// while remaining active.
type Mapping interface {
	// Returns a channel. One value will be sent on the channel whenever the
	// value returned by ExternalAddr() changes, unless the value previously sent
	// on the channel has yet to be consumed.
	NotifyChan() <-chan struct{}

	// Deletes the mapping. Doesn't block until the mapping is destroyed.
	Delete()

	// Returns the external address in "IP:port" format.
	// If the mapping is not active, returns an empty string.
	// The IP address may not be globally routable, for example in double-NAT cases.
	// If the external port has been mapped but the external IP cannot be determined,
	// returns ":port".
	ExternalAddr() string
}

const DefaultLifetime = 2 * time.Hour

// Creates a port mapping. The mapping process is continually attempted and
// maintained in the background, but the Mapping interface is returned
// immediately without blocking.
//
// As the mapping is attempted asynchronously in the background, the mapping
// will not be active when this function returns. Use the Mapping interface
// returned to determine when the mapping becomes active.
//
// A successful mapping is not guaranteed.
//
// See the Config struct and the Mapping interface for more information.
func New(cfg Config) (Mapping, error) {
	if IsGloballyRoutable() {
		return nil, ErrGlobalIP
	}

	gwa, err := gateway.GetIPs()
	if err != nil {
		return nil, err
	}

	if cfg.Lifetime == 0 {
		cfg.Lifetime = DefaultLifetime
	}

	m := &mapping{
		cfg:        cfg,
		abortChan:  make(chan struct{}),
		notifyChan: make(chan struct{}, 1),
	}

	ssdp.Start()
	go m.portMappingLoop(gwa)

	return m, nil
}

var ErrGlobalIP = fmt.Errorf("machine is on global internet, port mapping not required")

// Returns true if the machine has a globally routable IP and port mapping is
// thus not required.
func IsGloballyRoutable() bool {
	gr, _ := isGloballyRoutable()
	return gr
}

func isGloballyRoutable() (bool, net.IP) {
	ip, err := determineSelfIP()
	if err != nil {
		return false, nil
	}

	return ip.IsGlobalUnicast(), ip
}

// Figure out our own IP.
func determineSelfIP() (net.IP, error) {
	c, err := net.Dial("udp", "4.2.2.1")
	if err != nil {
		return nil, err
	}

	defer c.Close()

	uaddr := c.LocalAddr().(*net.UDPAddr)
	return uaddr.IP, nil
}

// Mapping.
type mapping struct {
	mutex sync.Mutex

	// m: Protected by mutex

	cfg Config // m(ExternalPort)

	expireTime time.Time // m

	aborted   bool          // m
	abortChan chan struct{} // m

	notifyChan chan struct{} // m

	externalAddr string // m
	prevValue    string
}

func (m *mapping) NotifyChan() <-chan struct{} {
	return m.notifyChan
}

func (m *mapping) Delete() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.aborted {
		return
	}

	close(m.abortChan)
	m.aborted = true
}

func (m *mapping) ExternalAddr() string {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.isActive() || m.cfg.ExternalPort == 0 {
		return ""
	}

	return net.JoinHostPort(m.externalAddr, strconv.FormatUint(uint64(m.cfg.ExternalPort), 10))
}

func (m *mapping) isActive() bool {
	return !m.expireTime.IsZero() && m.expireTime.After(time.Now())
}

func (m *mapping) lIsActive() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.isActive()
}

// © 2010 Jack Palevich          BSD License  (Taipei-Torrent)
// © 2013 John Beisley           MIT License  (huin/goupnp)
// © 2013 John Howard Palevich   Apache v2 License (go-nat-pmp)
// © 2014 Hugo Landau            MIT License
