package repository

import (
	"fmt"
	"strings"
)

// sqliteMemoryTestName turns a Go test name into a safe file-URI path segment.
//
// Test names are not URI-safe: an empty subtest name is reported as "#00", and
// an unescaped "#" starts a URI fragment. That silently drops the following
// "?mode=memory" parameter, so the driver creates a real database file in the
// package directory instead of an in-memory database. Reserved characters are
// therefore percent-encoded, and path separators are flattened to "_".
//
// WriteByte/WriteRune on strings.Builder never return an error, but errcheck
// still requires the result to be consumed.
func sqliteMemoryTestName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', ' ', '\\':
			_ = b.WriteByte('_')
		case '#', '?', '%', '&', ':', '"', '<', '>', '|', '*', '\'':
			_, _ = fmt.Fprintf(&b, "%%%02X", r)
		default:
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}

// sqliteMemoryDSN builds a shared-cache in-memory DSN for one test.
func sqliteMemoryDSN(name string, extraParams ...string) string {
	params := append([]string{"mode=memory", "cache=shared"}, extraParams...)
	return "file:" + sqliteMemoryTestName(name) + "?" + strings.Join(params, "&")
}
