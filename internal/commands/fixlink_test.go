package commands

import (
	"slices"
	"testing"
)

func TestFixXLinks(t *testing.T) {
	for text, want := range map[string][]string{
		"https://x.com/ShitpostGate/status/2106383869503054022?s=20": {"https://fixupx.com/ShitpostGate/status/2106383869503054022"},
		"lol https://twitter.com/a_b/status/1/photo/1 and https://www.x.com/c/status/2?t=xyz then https://mobile.twitter.com/a_b/status/1": {
			"https://fixupx.com/a_b/status/1", "https://fixupx.com/c/status/2",
		},
		"https://x.com/home https://fixupx.com/a/status/1 https://notx.com/a/status/3": nil,
	} {
		if got := fixXLinks(text); !slices.Equal(got, want) {
			t.Errorf("fixXLinks(%q) = %v, want %v", text, got, want)
		}
	}
}
