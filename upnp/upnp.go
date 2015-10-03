// Package upnp implements low level UPnP port mapping protocol implementation functions.
package upnp

import "net/http"
import "net/url"
import gnet "net"
import "encoding/xml"
import "errors"
import "fmt"
import "math/rand"
import "strings"
import "time"
import "html"

// Protocol Structures

const upnpDeviceNS = "urn:schemas-upnp-org:device-1-0"

type xRootDevice struct {
	XMLName xml.Name `xml:"root"`
	Device  xDevice  `xml:"device"`
}

type xDevice struct {
	Services []xService `xml:"serviceList>service,omitempty"`
	Devices  []xDevice  `xml:"deviceList>device,omitempty"`
}

func (self *xDevice) InitURLFields(base *url.URL) {
	for i := range self.Services {
		self.Services[i].InitURLFields(base)
	}
	for i := range self.Devices {
		self.Devices[i].InitURLFields(base)
	}
}

type servicesVisitorFunc func(s *xService)

func (self *xDevice) VisitServices(f servicesVisitorFunc) {
	for i := range self.Services {
		f(&self.Services[i])
	}
	for i := range self.Devices {
		self.Devices[i].VisitServices(f)
	}
}

type xService struct {
	ServiceType string    `xml:"serviceType"`
	ServiceID   string    `xml:"serviceId"`
	ControlURL  xURLField `xml:"controlURL"`
}

func (self *xService) InitURLFields(base *url.URL) {
	self.ControlURL.InitURLFields(base)
}

type xURLField struct {
	URL url.URL `xml:"-"`
	OK  bool    `xml:"-"`
	Str string  `xml:",chardata"`
}

func (self *xURLField) InitURLFields(base *url.URL) {
	u, err := url.Parse(self.Str)
	if err != nil {
		self.URL = url.URL{}
		self.OK = false
		return
	}

	self.URL = *base.ResolveReference(u)
	self.OK = true
}

type xSoapEnvelope struct {
	XMLName xml.Name  `xml:"Envelope"`
	Body    xSoapBody `xml:"Body"`
}

type xSoapBody struct {
	XMLName xml.Name `xml:"Body"`
	Data    []byte   `xml:",innerxml"`
}

type xGetExternalAddrResponse struct {
	XMLName           xml.Name `xml:"GetExternalIPAddressResponse"`
	ExternalIPAddress string   `xml:"NewExternalIPAddress"`
}

// Gets the WANIPConnection control URL from the main UPnP control URL.
func getWANIPControlURL(upnpURL string) (*url.URL, error) {
	urlp, err := url.Parse(upnpURL)
	if err != nil {
		return nil, err
	}

	res, err := http.Get(upnpURL)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != 200 {
		return nil, errors.New("non-200 status code when retrieving UPnP device description")
	}

	defer res.Body.Close()
	d := xml.NewDecoder(res.Body)
	d.DefaultSpace = upnpDeviceNS

	var root xRootDevice
	err = d.Decode(&root)
	if err != nil {
		return nil, err
	}

	root.Device.InitURLFields(urlp)

	var wurl *url.URL
	root.Device.VisitServices(func(s *xService) {
		if s.ServiceType != "urn:schemas-upnp-org:service:WANIPConnection:1" || wurl != nil || !s.ControlURL.OK {
			return
		}

		wurl = &s.ControlURL.URL
	})

	return wurl, nil
}

// Make a SOAP request to an URL.
func soapRequest(url, method, msg string) (*http.Response, error) {
	fm := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>` + msg + `</s:Body></s:Envelope>`

	req, err := http.NewRequest("POST", url, strings.NewReader(fm))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:WANIPConnection:1#`+method+`"`)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, errors.New("Non-successful HTTP error code")
	}

	return res, nil
}

// Figure out our own local IP relative to the gateway.
func determineSelfIP(u *url.URL) (gnet.IP, error) {
	c, err := gnet.Dial("udp", u.Host)
	if err != nil {
		return nil, err
	}

	defer c.Close()

	uaddr := c.LocalAddr().(*gnet.UDPAddr)
	return uaddr.IP, nil
}

func randInRange(low, high uint16) uint16 {
	return uint16(rand.Int31n(int32(high-low)) + int32(low))
}

// Performs a single UPnP transaction to map a port.
//
// Pass a UPnP device URL. The WANIPConnection endpoint will be located automatically.
func Map(upnpURL string, protocol Protocol, internalPort uint16,
	externalPort uint16, name string, duration time.Duration) (actualExternalPort uint16, err error) {
	wurl, err := getWANIPControlURL(upnpURL)

	if externalPort == 0 {
		externalPort = randInRange(1025, 65000)
	}

	selfIP, err := determineSelfIP(wurl)
	if err != nil {
		return 0, err
	}

	s := fmt.Sprintf(`<u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"><NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort><NewProtocol>%s</NewProtocol><NewInternalPort>%d</NewInternalPort><NewInternalClient>%s</NewInternalClient><NewEnabled>1</NewEnabled><NewPortMappingDescription>%s</NewPortMappingDescription><NewLeaseDuration>%d</NewLeaseDuration></u:AddPortMapping>`, externalPort, protocol.String(), internalPort, selfIP.String(), html.EscapeString(name), uint32(duration.Seconds()))

	res, err := soapRequest(wurl.String(), "AddPortMapping", s)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	// HTTP Status Code is non-200 if there was an error, so do we even need to check the body?

	return externalPort, nil
}

// Performs a single UPnP transaction to unmap a port.
//
// Pass a UPnP device URL. The WANIPConnection endpoint will be located automatically.
func Unmap(upnpURL string, protocol Protocol, externalPort uint16) error {
	wurl, err := getWANIPControlURL(upnpURL)

	s := fmt.Sprintf(`<u:DeletePortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"><NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort><NewProtocol>%s</NewProtocol></u:DeletePortMapping></s:Body></s:Envelope>`,
		externalPort, protocol.String())

	res, err := soapRequest(wurl.String(), "DeletePortMapping", s)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}

// Performs a single UPnP transaction to get the external address.
//
// Pass the UPnP device URL. The WANIPConnection endpoint will be located
// automatically.
func GetExternalAddr(upnpURL string) (ip gnet.IP, err error) {
	wurl, err := getWANIPControlURL(upnpURL)

	s := `<u:GetExternalIPAddress xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"/>`

	res, err := soapRequest(wurl.String(), "GetExternalIPAddress", s)
	if err != nil {
		return
	}
	defer res.Body.Close()

	var reply xSoapEnvelope
	err = xml.NewDecoder(res.Body).Decode(&reply)
	if err != nil {
		return
	}

	var reply2 xGetExternalAddrResponse
	err = xml.Unmarshal(reply.Body.Data, &reply2)
	if err != nil {
		return
	}

	ip = gnet.ParseIP(reply2.ExternalIPAddress)
	if ip == nil {
		err = fmt.Errorf("Unable to parse IP address")
		return
	}

	return
}

type Protocol int

const (
	TCP Protocol = 6
	UDP          = 17
)

func (p Protocol) String() string {
	switch p {
	case TCP:
		return "TCP"
	case UDP:
		return "UDP"
	default:
		panic("unknown protocol value")
	}
}
