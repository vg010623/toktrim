package tokenizer

import "testing"

func TestTokenizer_Count(t *testing.T) {
	tok, err := NewTokenizer()
	if err != nil {
		t.Fatalf("failed to create tokenizer: %v", err)
	}
	tests := []struct {
		text string
		want int
	}{
		{text: "", want: 0},
		{text: "hello world", want: 2}, // approximate; actual tokenization may differ but we trust tiktoken
		{text: "foo bar baz", want: 3},
	}
	for _, tt := range tests {
		if got := tok.Count(tt.text); got != tt.want {
			t.Errorf("Tokenizer.Count(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}