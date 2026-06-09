package u8proto

import (
	"testing"
)

func TestU8protoString(t *testing.T) {
	tests := []struct {
		proto U8proto
		want  string
	}{
		{ANY, "ANY"},
		{ICMP, "ICMP"},
		{TCP, "TCP"},
		{UDP, "UDP"},
		{ICMPv6, "ICMPv6"},
		{U8proto(99), "99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.proto.String(); got != tt.want {
				t.Errorf("U8proto.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseProtocol(t *testing.T) {
	tests := []struct {
		input string
		want  U8proto
		err   bool
	}{
		{"all", ANY, false},
		{"icmp", ICMP, false},
		{"tcp", TCP, false},
		{"udp", UDP, false},
		{"icmpv6", ICMPv6, false},
		{"unknown", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseProtocol(tt.input)
			if (err != nil) != tt.err {
				t.Errorf("ParseProtocol() error = %v, wantErr %v", err, tt.err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}