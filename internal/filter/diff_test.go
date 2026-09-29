package filter

import "testing"

func TestDiffFilter_Apply(t *testing.T) {
	f := DiffFilter{}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "a large lockfile diff collapses, a source diff does not",
			input: "diff --git a/package-lock.json b/package-lock.json\n" +
				"index 87654321..12345678 100644\n" +
				"--- a/package-lock.json\n" +
				"+++ b/package-lock.json\n" +
				"@@ -1,9 +1,9 @@\n" +
				"-  \"a\": \"1\",\n+  \"a\": \"2\",\n" +
				"-  \"b\": \"1\",\n+  \"b\": \"2\",\n" +
				"-  \"c\": \"1\",\n+  \"c\": \"2\",\n" +
				"diff --git a/src/index.js b/src/index.js\n" +
				"index 11111111..22222222 100644\n" +
				"--- a/src/index.js\n" +
				"+++ b/src/index.js\n" +
				"@@ -10,1 +10,1 @@\n" +
				"-console.log(\"foo\");\n" +
				"+console.log(\"bar\");\n",
			expected: "[Lockfile changed: package-lock.json (6 lines changed)]\n" +
				"diff --git a/src/index.js b/src/index.js\n" +
				"index 11111111..22222222 100644\n" +
				"--- a/src/index.js\n" +
				"+++ b/src/index.js\n" +
				"@@ -10,1 +10,1 @@\n" +
				"-console.log(\"foo\");\n" +
				"+console.log(\"bar\");\n",
		},
		{
			name: "a small lockfile diff is passed through",
			input: "diff --git a/yarn.lock b/yarn.lock\n" +
				"@@ -1,1 +1,1 @@\n" +
				"-foo@1.0.0\n" +
				"+foo@1.0.1\n",
			expected: "diff --git a/yarn.lock b/yarn.lock\n" +
				"@@ -1,1 +1,1 @@\n" +
				"-foo@1.0.0\n" +
				"+foo@1.0.1\n",
		},
		{
			name:     "non-diff output is untouched",
			input:    "normal line\nanother line\n",
			expected: "normal line\nanother line\n",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Apply(tt.input); got != tt.expected {
				t.Errorf("DiffFilter.Apply() = %q, want %q", got, tt.expected)
			}
		})
	}
}
