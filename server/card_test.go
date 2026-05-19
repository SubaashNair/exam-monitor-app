package main

import (
	"testing"
	"time"
)

func TestConnectionStateClassification(t *testing.T) {
	cases := []struct {
		name      string
		sinceLast time.Duration
		wantState ConnState
	}{
		{"fresh frame", 100 * time.Millisecond, ConnStateLive},
		{"just at 3s threshold", 3 * time.Second, ConnStateStale},
		{"in grace window", 5 * time.Second, ConnStateStale},
		{"just before 8s threshold", 7 * time.Second, ConnStateStale},
		{"at 8s threshold", 8 * time.Second, ConnStateLost},
		{"long gone", 30 * time.Second, ConnStateLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := time.Now().Add(-tc.sinceLast)
			got := classifyConnection(ts)
			if got != tc.wantState {
				t.Errorf("classifyConnection(-%v) = %v, want %v", tc.sinceLast, got, tc.wantState)
			}
		})
	}
}

func TestConnectionStateClassification_Zero(t *testing.T) {
	if got := classifyConnection(time.Time{}); got != ConnStateLost {
		t.Errorf("classifyConnection(zero) = %v, want ConnStateLost", got)
	}
}
