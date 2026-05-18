package main

import "testing"

// TestRoomNameToPort_ContractWithServer pins the inputs/outputs that both
// client and server MUST agree on. If you change the algorithm here, change
// it identically in server/util.go AND update this table in BOTH places.
func TestRoomNameToPort_ContractWithServer(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 12000},
		{"alpha", "Alpha", 12001},
		{"alpha lowercase", "alpha", 12001},
		{"alpha trimmed", "  Alpha  ", 12001},
		{"bravo", "Bravo", 12002},
		{"charlie", "Charlie", 12003},
		{"delta", "Delta", 12004},
		{"custom math 101", "Math 101", 12000 + int(fnvHash("math 101"))%50000},
		{"custom unicode", "教室", 12000 + int(fnvHash("教室"))%50000},
		{"port range floor", "x", 12000 + int(fnvHash("x"))%50000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RoomNameToPort(tc.in)
			if got != tc.want {
				t.Errorf("RoomNameToPort(%q) = %d, want %d", tc.in, got, tc.want)
			}
			if got < 12000 || got > 61999 {
				t.Errorf("RoomNameToPort(%q) = %d, out of [12000,61999]", tc.in, got)
			}
		})
	}
}

// fnvHash mirrors the algorithm in RoomNameToPort so the test can compute
// expected values for non-preset inputs without hard-coding.
func fnvHash(s string) uint32 {
	const offset32 = 2166136261
	const prime32 = 16777619
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}
