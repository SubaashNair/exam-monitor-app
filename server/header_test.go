package main

import (
	"encoding/binary"
	"testing"
)

func makeHeader(magic string, typ uint16, length uint32) []byte {
	buf := make([]byte, HEADER_SIZE)
	copy(buf, magic)
	if len(magic) >= 2 {
		binary.BigEndian.PutUint16(buf[2:4], typ)
		binary.BigEndian.PutUint32(buf[4:8], length)
	}
	return buf
}

func TestUnpackHeader(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantType   uint16
		wantLength int
		wantErr    bool
	}{
		{
			name:       "valid picture frame",
			data:       makeHeader("HE", 2, 1024),
			wantType:   2,
			wantLength: 1024,
			wantErr:    false,
		},
		{
			name:       "valid name frame",
			data:       makeHeader("HE", 0, 32),
			wantType:   0,
			wantLength: 32,
			wantErr:    false,
		},
		{
			name:       "valid message frame zero length",
			data:       makeHeader("HE", 1, 0),
			wantType:   1,
			wantLength: 0,
			wantErr:    false,
		},
		{
			name:       "valid header with trailing data ignored",
			data:       append(makeHeader("HE", 2, 5), []byte("extra")...),
			wantType:   2,
			wantLength: 5,
			wantErr:    false,
		},
		{
			name:       "valid max-uint32 length parses",
			data:       makeHeader("HE", 0, 0xFFFFFFFF),
			wantType:   0,
			wantLength: int(uint32(0xFFFFFFFF)),
			wantErr:    false,
		},
		{
			name:    "wrong magic XY",
			data:    makeHeader("XY", 2, 100),
			wantErr: true,
		},
		{
			name:    "lowercase magic he is rejected",
			data:    makeHeader("he", 2, 100),
			wantErr: true,
		},
		{
			name:    "empty input",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "seven bytes (one short)",
			data:    []byte("HEABCDE"),
			wantErr: true,
		},
		{
			name:    "nil input",
			data:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotType, gotLength, err := unpackHeader(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unpackHeader() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotType != tt.wantType {
				t.Errorf("type = %d, want %d", gotType, tt.wantType)
			}
			if gotLength != tt.wantLength {
				t.Errorf("length = %d, want %d", gotLength, tt.wantLength)
			}
		})
	}
}
