package tokenizer

import "testing"

func TestEstimate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"shorter than one token", "ab", 1},
		{"exactly one token", "abcd", 1},
		{"multiple tokens", "0123456789ab", 3},
		{"remainder is truncated", "0123456789abc", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Estimate(tt.in); got != tt.want {
				t.Errorf("Estimate(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestEstimateNeverNegative(t *testing.T) {
	for _, s := range []string{"", "a", "aa", "aaa"} {
		if got := Estimate(s); got < 0 {
			t.Errorf("Estimate(%q) = %d, want >= 0", s, got)
		}
	}
}
