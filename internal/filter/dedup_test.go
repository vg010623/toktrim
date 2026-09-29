package filter

import "testing"

func TestDedupFilter_Apply(t *testing.T) {
	f := DedupFilter{}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "runs of three or more collapse, shorter runs survive",
			input:    "line1\nline1\nline1\nline2\nline2\nline3\n",
			expected: "[Repeated 3 times: line1]\nline2\nline2\nline3\n",
		},
		{
			name:     "a single long run collapses to one line",
			input:    "a\na\na\na\na\n",
			expected: "[Repeated 5 times: a]\n",
		},
		{
			name:     "single line is unchanged",
			input:    "single line\n",
			expected: "single line\n",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "no trailing newline is not invented",
			input:    "only line",
			expected: "only line",
		},
		{
			name:     "trailing newline is preserved exactly once",
			input:    "hi\n",
			expected: "hi\n",
		},
		{
			name:     "blank line runs are left alone",
			input:    "a\n\n\n\nb\n",
			expected: "a\n\n\n\nb\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Apply(tt.input); got != tt.expected {
				t.Errorf("DedupFilter.Apply() = %q, want %q", got, tt.expected)
			}
		})
	}
}
