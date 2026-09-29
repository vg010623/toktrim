package filter

// Filter defines the interface for output filters.
// Each filter takes a string and returns a filtered string.
type Filter interface {
	Apply(input string) string
}
