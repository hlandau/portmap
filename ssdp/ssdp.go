// SSDP registry. Receives SSDP events from package ssdp and stores them for
// retrieval. You can use this package to discover services using SSDP.
package ssdp

import "net/url"
import "github.com/hlandau/portmap/ssdp/ssdpbase"
import "github.com/hlandau/degoutils/log"
import "time"
import "sync"

// Describes a service discovered by SSDP.
type Service struct {
	// An URL describing the location of the service.
	Location *url.URL

	// The service type string.
	ST string

	// A unique serial number for the service.
	USN string

	// The time at which a notice for this service was last seen.
	LastSeen time.Time
}

var once sync.Once
var client ssdpbase.Client
var byUSN = map[string]*Service{}

func loop() {
	for ev := range client.Chan() {
		if _, already := byUSN[ev.USN]; !already {
			byUSN[ev.USN] = &Service{USN: ev.USN}
		}

		svc := byUSN[ev.USN]
		svc.ST = ev.ST
		svc.Location = ev.Location
		svc.LastSeen = time.Now()

		//log.Info("Registering SSDP service: ", svc)
	}
}

// Starts the SSDP discovery broadcast and notice reception process, if it has
// not already started. You may call this function multiple times without
// consequence.
func Start() {
	once.Do(func() {
		var err error
		client, err = ssdpbase.NewClient()
		log.Panice(err)

		go loop()
	})
}

// Obtains a list of Services matching the provided Service Type string.
//
// Note that if you call Start() for the first time immediately prior to
// calling this, this may return an empty list even if services are available,
// as it may take a moment for devices to respond to the initial discovery
// broadcast.
//
// Services which were last seen more than three SSDP broadcast intervals ago
// are not yielded by this function.
func GetServicesByType(st string) (svcs []Service) {
	limit := time.Now().Add(ssdpbase.BroadcastInterval * -3)
	for _, v := range byUSN {
		if v.ST == st && v.LastSeen.After(limit) {
			svcs = append(svcs, *v)
		}
	}
	return
}
