// Low-level SSDP package which provides a channel streaming SSDP events.
//
// Use package ssdp instead of this package.
package ssdpbase

import gnet "net"
import "time"
import "github.com/hlandau/degoutils/net"
import "net/http"
import "bytes"
import "net/url"
import "bufio"

// Interval at which discovery beacons are sent.
const BroadcastInterval = 60 * time.Second

// Represents a received SSDP beacon.
type Event struct {
	Location *url.URL
	ST       string
	USN      string
}

// SSDP event receiver.
type Client interface {
	// Returns a channel used to receive events.
	Chan() <-chan Event

	// Stops the receiver.
	Stop()
}

type client struct {
	conn      *gnet.UDPConn
	eventChan chan Event
	stopChan  chan struct{}
}

func (c *client) Stop() {
	close(c.stopChan)
	close(c.eventChan)
	c.conn.Close()
}

func (c *client) Chan() <-chan Event {
	return c.eventChan
}

func (c *client) broadcastLoop() {
	defer c.conn.Close()

	ssdpAddr, err := gnet.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if err != nil {
		return
	}

	ticker := time.NewTicker(BroadcastInterval)
	defer ticker.Stop()

	discoBuf := []byte(
		"M-SEARCH * HTTP/1.1\r\n" +
			"HOST: 239.255.255.250:1900\r\n" +
			"ST: ssdp:all\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: 2\r\n\r\n")

	for {
		c.conn.WriteToUDP(discoBuf, ssdpAddr) // ignore errors
		select {
		case <-ticker.C:
		case <-c.stopChan:
			return
		}
	}
}

func (c *client) handleResponse(res *http.Response) {
	if res.StatusCode != 200 {
		return
	}

	st := res.Header.Get("ST")
	if st == "" {
		return
	}

	loc, err := res.Location()
	if err != nil {
		return
	}

	usn := res.Header.Get("USN")
	if usn == "" {
		usn = loc.String()
	}

	ev := Event{
		Location: loc,
		ST:       st,
		USN:      usn,
	}

	select {
	// events not being waited for are simply dropped
	case c.eventChan <- ev:
	default:
	}
}

func (c *client) recvLoop() {
	for {
		buf, _, err := net.ReadDatagramFromUDP(c.conn)
		if err != nil {
			return
		}

		rbio := bufio.NewReader(bytes.NewReader(buf))
		res, err := http.ReadResponse(rbio, nil)
		if err == nil {
			c.handleResponse(res)
		}
	}
}

func NewClient() (Client, error) {
	conng, err := gnet.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}

	conn := conng.(*gnet.UDPConn)

	c := &client{
		stopChan:  make(chan struct{}),
		eventChan: make(chan Event, 10),
		conn:      conn,
	}

	go c.broadcastLoop()
	go c.recvLoop()

	return c, nil
}
