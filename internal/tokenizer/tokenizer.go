package tokenizer

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
)

// Tokenizer counts tokens using tiktoken-go with cl100k_base encoding (GPT-4o compatible).
// In Go, we initialize the encoder once and reuse it.
// Compared to TypeScript: Go encourages reuse of expensive resources via init or constructor functions.
type Tokenizer struct {
	enc *tiktoken.Tiktoken
}

// NewTokenizer creates a Tokenizer initialized with cl100k_base encoding.
func NewTokenizer() (*Tokenizer, error) {
	enc, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, fmt.Errorf("failed to load cl100k_base encoding: %w", err)
	}
	return &Tokenizer{enc: enc}, nil
}

// Count returns the number of tokens in the given text.
func (t *Tokenizer) Count(text string) int {
	return len(t.Encodify(text))
}

// Encodify returns the token IDs for the text (useful for debugging).
func (t *Tokenizer) Encodify(text string) []int {
	return t.enc.Encode(text, nil, nil)
}

// Encode is alias for Encodify.
func (t *Tokenizer) Encode(text string) []int {
	return t.Encodify(text)
}

// Decode returns string from token IDs.
func (t *Tokenizer) Decode(tokens []int) string {
	return t.enc.Decode(tokens)
}
