// Package tokenizer provides a dependency-free, offline token estimate.
//
// toktrim deliberately does not use a real BPE tokenizer: every Go
// implementation either bundles a large vocabulary or downloads one at
// runtime, and toktrim must work with the network disabled. The
// chars/4 heuristic is within a few percent of cl100k_base on log-shaped
// text, which is all that is needed to report how much was trimmed.
package tokenizer

// CharsPerToken is the divisor used by the estimate.
const CharsPerToken = 4

// Estimate returns the approximate number of tokens in text.
// The result is an estimate and is always labelled as such when reported.
func Estimate(text string) int {
	if text == "" {
		return 0
	}
	n := len(text) / CharsPerToken
	if n == 0 {
		return 1
	}
	return n
}

// EstimateBytes returns the approximate number of tokens in n bytes of text.
func EstimateBytes(n int) int {
	if n <= 0 {
		return 0
	}
	t := n / CharsPerToken
	if t == 0 {
		return 1
	}
	return t
}
