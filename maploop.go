package portmap

import "net"
import "time"
import "github.com/hlandau/portmap/ssdp"
import "github.com/hlandau/portmap/upnp"
import "github.com/hlandau/portmap/natpmp"
import "github.com/hlandau/xlog"

var log, Log = xlog.NewQuiet("portmap")

type mode int

const (
	modeNATPMP mode = iota
	modeUPnP
)

func (m *mapping) portMappingLoop(gwa []net.IP) {
	aborting := false
	mode := modeNATPMP
	var ok bool
	var d time.Duration
	for {
		// Already inactive (e.g. expired or was never active), so no need to do anything.
		if aborting && !m.lIsActive() {
			return
		}

		switch mode {
		case modeNATPMP:
			ok = m.tryNATPMP(gwa, aborting)
			if ok {
				d = m.cfg.Lifetime / 2
			} else {
				svc := ssdp.GetServicesByType(upnpWANIPConnectionURN)
				if len(svc) > 0 {
					// NAT-PMP failed and UPnP is available, so switch to it
					mode = modeUPnP
					log.Debug("NAT-PMP failed and UPnP is available, switching to UPnP")
					continue
				}
			}

		case modeUPnP:
			svcs := ssdp.GetServicesByType(upnpWANIPConnectionURN)
			if len(svcs) == 0 {
				mode = modeNATPMP
				log.Debug("UPnP not available, switching to NAT-PMP")
				continue
			}

			ok = m.tryUPnP(svcs, aborting)
			d = 1 * time.Hour
		}

		// If we are aborting, then the call we just made was to remove the mapping,
		// not set it, and we're done.
		if aborting {
			m.setInactive()
			return
		}

		// Backoff
		if ok {
			m.cfg.Backoff.Reset()
		} else {
			// failed, do retry delay
			d = m.cfg.Backoff.NextDelay()
			if d == 0 {
				// max tries occurred
				m.setInactive()
				return
			}
		}

		m.notify()

		select {
		case <-m.abortChan:
			aborting = true

		case <-time.After(d):
			// wait until we need to renew
		}
	}
}

func (m *mapping) notify() {
	ea := m.ExternalAddr()

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.prevValue == ea {
		// no change
		return
	}

	m.prevValue = ea

	select {
	case m.notifyChan <- struct{}{}:
	default:
	}
}

// NAT-PMP

func (m *mapping) tryNATPMP(gwa []net.IP, destroy bool) bool {
	for _, gw := range gwa {
		if m.tryNATPMPGW(gw, destroy) {
			return true
		}
	}
	return false
}

func (m *mapping) tryNATPMPGW(gw net.IP, destroy bool) bool {
	var externalPort uint16
	var actualLifetime time.Duration
	var err error

	var preferredLifetime time.Duration
	if destroy && !m.lIsActive() {
		// no point destroying if we're not active
		return true
	} else if !destroy {
		// lifetime is zero if we're destroying
		preferredLifetime = m.cfg.Lifetime
	}

	// attempt mapping
	externalPort, actualLifetime, err = natpmp.Map(gw,
		natpmp.Protocol(m.cfg.Protocol), m.cfg.InternalPort, m.cfg.ExternalPort, preferredLifetime)
	if err != nil {
		log.Infof("NAT-PMP failed: %v", err)
		return false
	}

	m.mutex.Lock()
	m.cfg.ExternalPort = externalPort
	m.cfg.Lifetime = actualLifetime
	m.mutex.Unlock()
	if preferredLifetime == 0 {
		// we have finished tearing down the mapping by mapping it with a
		// lifetime of zero, so return
		return true
	}

	expireTime := time.Now().Add(actualLifetime)

	// Now attempt to get the external IP.
	extIP, err := natpmp.GetExternalAddr(gw)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// update external address
	if err == nil {
		m.externalAddr = extIP.String()
	}

	m.expireTime = expireTime
	return true
}

// UPnP

func (m *mapping) tryUPnP(svcs []ssdp.Service, destroy bool) bool {
	for _, svc := range svcs {
		if m.tryUPnPSvc(svc, destroy) {
			return true
		}
	}
	return false
}

const upnpWANIPConnectionURN = "urn:schemas-upnp-org:service:WANIPConnection:1"

func (m *mapping) tryUPnPSvc(svc ssdp.Service, destroy bool) bool {
	if destroy {
		// unmapping
		if !m.lIsActive() {
			return true
		}

		err := upnp.Unmap(svc.Location.String(), upnp.Protocol(m.cfg.Protocol), m.cfg.ExternalPort)
		return err == nil
	}

	// mapping
	actualExternalPort, err := upnp.Map(svc.Location.String(), upnp.Protocol(m.cfg.Protocol),
		m.cfg.InternalPort,
		m.cfg.ExternalPort, m.cfg.Name, m.cfg.Lifetime)

	if err != nil {
		return false
	}

	m.mutex.Lock()
	m.expireTime = time.Now().Add(m.cfg.Lifetime)
	m.cfg.ExternalPort = actualExternalPort
	m.mutex.Unlock()

	// Now attempt to get the external IP.
	if destroy {
		return true
	}

	extIP, err := upnp.GetExternalAddr(svc.Location.String())
	if err != nil {
		// mapping till succeeded
		return true
	}

	// update external address
	m.mutex.Lock()
	m.externalAddr = extIP.String()
	m.mutex.Unlock()

	return true
}

//

func (m *mapping) setInactive() {
	m.mutex.Lock()
	m.expireTime = time.Time{}
	m.mutex.Unlock()

	m.notify()
}
