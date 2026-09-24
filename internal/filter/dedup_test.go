package filter

import "testing"

func TestDedupFilter_Apply(t *testing.T) {
	f := DedupFilter{}
	tests := []struct {
		input    string
		expected string
	}{
		{
			input: `line1
line1
line1
line2
line2
line3
`,
			expected: `[Repeated 3 times: line1]
line1
line1
[Repeated 2 times: line2]
line2
line3
`,
		},
		{
			input: `a
a
a
a
a
`,
			expected: `[Repeated 5 times: a]
a
a
a
a
a
`,
		},
		{
			input: `single line
`,
			expected: `single line
`,
		},
		{
			input: ``,
			expected: ``,
		},
	}
	for _, tt := range tests {
		if got := f.Apply(tt.input); got != tt.expected {
			t.Errorf("DedupFilter.Apply() = %q, want %q", got, tt.expected)
		}
	}
}