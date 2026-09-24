package filter

import "testing"

func TestDiffFilter_Apply(t *testing.T) {
	f := DiffFilter{}
	tests := []struct {
		input    string
		expected string
	}{
		{
			input: `diff --git a/package-lock.json b/package-lock.json
index 87654321..12345678 100644
--- a/package-lock.json
+++ b/package-lock.json
@@ -1,5 +1,5 @@
 {
   "name": "myapp",
-  "version": "1.0.0",
+  "version": "1.0.1",
 }
diff --git a/src/index.js b/src/index.js
index 11111111..22222222 100644
--- a/src/index.js
+++ b/src/index.js
@@ -10,1 +10,1 @@
-console.log("foo");
+console.log("bar");
`,
			expected: `[Lockfile changed: package-lock.json (4 lines changed)]
diff --git a/src/index.js b/src/index.js
index 11111111..22222222 100644
--- a/src/index.js
+++ b/src/index.js
@@ -10,1 +10,1 @@
-console.log("foo");
+console.log("bar")
`,
		},
		{
			input: `normal line
another line
`,
			expected: `normal line
another line
`,
		},
	}
	for _, tt := range tests {
		if got := f.Apply(tt.input); got != tt.expected {
			t.Errorf("DiffFilter.Apply() = %q, want %q", got, tt.expected)
		}
	}
}