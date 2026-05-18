package main

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestParseManualIP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIP  string
		wantErr bool
	}{
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"ipv4", "192.168.1.10", "192.168.1.10", false},
		{"ipv4 with spaces", "  10.0.0.5  ", "10.0.0.5", false},
		{"ipv4 with port stripped", "192.168.1.10:8080", "192.168.1.10", false},
		{"ipv6 loopback", "::1", "::1", false},
		{"garbage", "not.an.ip", "", true},
		{"empty octets", "...", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseManualIP(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseManualIP(%q) err=%v wantErr=%v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.wantIP {
				t.Errorf("parseManualIP(%q) = %q; want %q", tt.input, got, tt.wantIP)
			}
		})
	}
}

func TestResolveServer_PrefersManualOverEverything(t *testing.T) {
	r := &resolver{
		manualIP: "10.1.1.1",
		mdns: func(_ int, _ time.Duration) (string, error) {
			t.Fatal("mdns should not be called when manual IP is set")
			return "", nil
		},
		broadcast: func(_ int, _ time.Duration) (string, error) {
			t.Fatal("broadcast should not be called when manual IP is set")
			return "", nil
		},
	}

	ip, source, err := r.Resolve(8080)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ip != "10.1.1.1" {
		t.Errorf("ip = %q; want 10.1.1.1", ip)
	}
	if source != "manual" {
		t.Errorf("source = %q; want manual", source)
	}
}

func TestResolveServer_FallsBackFromMDNSToBroadcast(t *testing.T) {
	mdnsCalled := false
	broadcastCalled := false

	r := &resolver{
		manualIP: "",
		mdns: func(_ int, _ time.Duration) (string, error) {
			mdnsCalled = true
			return "", errors.New("no mdns response")
		},
		broadcast: func(_ int, _ time.Duration) (string, error) {
			broadcastCalled = true
			return "192.168.1.50", nil
		},
	}

	ip, source, err := r.Resolve(8080)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !mdnsCalled || !broadcastCalled {
		t.Errorf("expected both mdns and broadcast to be tried; mdns=%v bcast=%v", mdnsCalled, broadcastCalled)
	}
	if ip != "192.168.1.50" || source != "broadcast" {
		t.Errorf("got ip=%q source=%q; want 192.168.1.50/broadcast", ip, source)
	}
}

func TestResolveServer_MDNSWinsWhenAvailable(t *testing.T) {
	r := &resolver{
		manualIP: "",
		mdns: func(_ int, _ time.Duration) (string, error) {
			return "172.16.0.100", nil
		},
		broadcast: func(_ int, _ time.Duration) (string, error) {
			t.Fatal("broadcast should not be called when mdns succeeds")
			return "", nil
		},
	}

	ip, source, err := r.Resolve(8080)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ip != "172.16.0.100" || source != "mdns" {
		t.Errorf("got ip=%q source=%q; want 172.16.0.100/mdns", ip, source)
	}
}

func TestResolveServer_AllFailReturnsError(t *testing.T) {
	r := &resolver{
		manualIP: "",
		mdns: func(_ int, _ time.Duration) (string, error) {
			return "", errors.New("no mdns")
		},
		broadcast: func(_ int, _ time.Duration) (string, error) {
			return "", errors.New("no broadcast")
		},
	}

	_, _, err := r.Resolve(8080)
	if err == nil {
		t.Fatal("expected error when both discovery paths fail")
	}
}

func TestResolveServer_InvalidManualIPFallsThrough(t *testing.T) {
	r := &resolver{
		manualIP: "not-an-ip",
		mdns: func(_ int, _ time.Duration) (string, error) {
			return "192.168.1.99", nil
		},
		broadcast: func(_ int, _ time.Duration) (string, error) {
			t.Fatal("broadcast should not be reached")
			return "", nil
		},
	}

	ip, source, err := r.Resolve(8080)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ip != "192.168.1.99" || source != "mdns" {
		t.Errorf("invalid manual IP should not block fallback; got ip=%q source=%q", ip, source)
	}
}

func TestParseManualIP_IPv6BracketHostPort(t *testing.T) {
	// Sanity-check the standard library's host-port behavior on bracketed IPv6.
	host, _, err := net.SplitHostPort("[::1]:8080")
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if host != "::1" {
		t.Errorf("expected ::1, got %q", host)
	}
}
