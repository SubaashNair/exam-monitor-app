package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

// mdnsServiceName is the Bonjour/Avahi service type advertised by the server.
// The room/port is embedded in TXT records and the SRV port field.
const mdnsServiceName = "_examguard._tcp"
const mdnsDomain = "local."

// discoverFn is a unit-testable seam for the two network-discovery strategies.
type discoverFn func(port int, timeout time.Duration) (string, error)

// resolver runs the three discovery strategies in order: manual IP first, then
// mDNS, then UDP broadcast. The mdns and broadcast hooks are injected so tests
// can drive the resolution graph without hitting the network.
type resolver struct {
	manualIP  string
	mdns      discoverFn
	broadcast discoverFn
}

// Resolve returns (ip, source, error). source is one of "manual", "mdns",
// "broadcast" — exposed so the diag log records which path actually worked.
func (r *resolver) Resolve(port int) (string, string, error) {
	if ip, err := parseManualIP(r.manualIP); err == nil {
		return ip, "manual", nil
	}

	mdnsTimeout := 3 * time.Second
	if ip, err := r.mdns(port, mdnsTimeout); err == nil && ip != "" {
		return ip, "mdns", nil
	} else if err != nil {
		slog.Debug("mdns discovery failed", "err", err)
	}

	broadcastTimeout := 10 * time.Second
	if ip, err := r.broadcast(port, broadcastTimeout); err == nil && ip != "" {
		return ip, "broadcast", nil
	} else if err != nil {
		slog.Debug("broadcast discovery failed", "err", err)
	}

	return "", "", errors.New("no server found via manual IP, mDNS, or broadcast")
}

// parseManualIP normalises the user-typed IP field from JoinView. Accepts
// bare IPs, IP:port, [ipv6]:port. Returns the host part stripped of port
// and surrounding whitespace.
func parseManualIP(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("empty")
	}
	// Try host:port first; if that parses, use the host.
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	return "", fmt.Errorf("not a valid IP: %q", raw)
}

// discoverMDNS browses the local network for the server's advertised service.
// Returns the first IPv4 address found, or an error on timeout / no results.
func discoverMDNS(port int, timeout time.Duration) (string, error) {
	r, err := zeroconf.NewResolver(nil)
	if err != nil {
		return "", fmt.Errorf("mdns resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 4)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := r.Browse(ctx, mdnsServiceName, mdnsDomain, entries); err != nil {
		return "", fmt.Errorf("mdns browse: %w", err)
	}

	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				return "", errors.New("mdns: no entries before channel close")
			}
			if entry == nil {
				continue
			}
			// Match the room/port from the SRV record.
			if port != 0 && int(entry.Port) != port {
				continue
			}
			for _, ip := range entry.AddrIPv4 {
				if ip != nil && !ip.IsUnspecified() {
					return ip.String(), nil
				}
			}
		case <-ctx.Done():
			return "", fmt.Errorf("mdns timeout after %v", timeout)
		}
	}
}
