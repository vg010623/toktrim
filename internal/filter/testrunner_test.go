package filter

import "testing"

func TestTestRunnerFilter_Apply(t *testing.T) {
	f := TestRunnerFilter{}
	tests := []struct {
		input    string
		expected string
	}{
		{
			input: `PASS test/utils.test.js
 PASS test/foo.test.js
 FAIL test/bar.test.js
  expect(received).toBe(expected) // Expected: "foo", Received: "bar"
    at Object.<anonymous> (test/bar.test.js:10:22)
 PASS test/baz.test.js
`,
			expected: ` FAIL test/bar.test.js
  expect(received).toBe(expected) // Expected: "foo", Received: "bar"
    at Object.<anonymous> (test/bar.test.js:10:22)
`,
		},
		{
			input: `✓ test1 passed
✓ test2 passed
✓ test3 passed
`,
			expected: "",
		},
		{
			input: `Downloading package foo@1.2.3
Downloading package bar@4.5.6
Compiling baz
`,
			expected: "",
		},
		{
			input: `some random output
that should be kept
`,
			expected: `some random output
that should be kept
`,
		},
	}
	for _, tt := range tests {
		if got := f.Apply(tt.input); got != tt.expected {
			t.Errorf("TestRunnerFilter.Apply() = %q, want %q", got, tt.expected)
		}
	}
}
