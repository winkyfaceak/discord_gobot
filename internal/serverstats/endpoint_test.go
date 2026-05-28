package serverstats

import "testing"

func TestParseEndpointAcceptsPublicLiteralAddresses(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"203.0.113.10:27015", "203.0.113.10:27015"},
		{"[2001:db8::10]:27015", "[2001:db8::10]:27015"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			endpoint, err := ParseEndpoint(test.input)
			if err != nil {
				t.Fatalf("ParseEndpoint(%q) error = %v", test.input, err)
			}
			if endpoint.Address != test.want {
				t.Fatalf("ParseEndpoint(%q).Address = %q, want %q", test.input, endpoint.Address, test.want)
			}
		})
	}
}

func TestParseEndpointRejectsUnsafeOrMalformedTargets(t *testing.T) {
	inputs := []string{
		"",
		"server.example.com:27015",
		"8.8.8.8",
		"8.8.8.8:0",
		"127.0.0.1:27015",
		"10.1.2.3:27015",
		"169.254.1.2:27015",
		"100.64.10.20:27015",
		"224.0.0.1:27015",
		"[::1]:27015",
		"[fc00::1]:27015",
		"[fe80::1]:27015",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseEndpoint(input); err == nil {
				t.Fatalf("ParseEndpoint(%q) succeeded, want rejection", input)
			}
		})
	}
}
