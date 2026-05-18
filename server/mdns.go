package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/grandcat/zeroconf"
)

const mdnsServiceName = "_examguard._tcp"

// advertiseMDNS registers the server's room/port via mDNS so clients on the
// same LAN can discover it without UDP broadcast (which is filtered by many
// firewalls). Returns a shutdown func; safe to call shutdown if Register failed
// — it's a no-op on nil.
func advertiseMDNS(port int) func() {
	host, err := os.Hostname()
	if err != nil {
		host = "exam-monitor"
	}
	instance := fmt.Sprintf("%s-room-%d", host, port)
	server, err := zeroconf.Register(
		instance,
		mdnsServiceName,
		"local.",
		port,
		[]string{fmt.Sprintf("room=%d", port)},
		nil,
	)
	if err != nil {
		slog.Warn("mdns register failed; clients on this LAN can still find via UDP broadcast or manual IP",
			"err", err, "port", port)
		return func() {}
	}
	slog.Info("mdns advertising", "service", mdnsServiceName, "instance", instance, "port", port)
	return func() {
		server.Shutdown()
		slog.Info("mdns advertise stopped", "port", port)
	}
}
