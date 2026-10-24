package uiaccess

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUIAccessDomainNormalize(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {" \t\r\n", ""}, {" Example.COM. ", "example.com"},
		{"API.Example.COM", "api.example.com"}, {"例子.中国", "xn--fsqu00a.xn--fiqs8s"},
		{"bücher.example", "xn--bcher-kva.example"},
		{strings.Repeat("a", 63) + ".example", strings.Repeat("a", 63) + ".example"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeDomain(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, input := range []string{
		"https://example.com", "http://example.com", "//example.com", "example.com/path",
		"example.com:443", "user@example.com", "*.example.com", "example.com?token=x",
		"example.com#hash", "example.com\\evil", "example..com", "example.com..",
		"-example.com", "example-.com", "foo_bar.example", "a b.example", "a\tb.example",
		"localhost", "127.0.0.1", "[::1]", "::1", "127.1", "123.456", "foo.0x1",
		"evil\".com", "<script>.com", "a\x00.example", strings.Repeat("a", 64) + ".com",
		strings.Repeat(strings.Repeat("a", 63)+".", 4) + "com",
	} {
		t.Run("invalid/"+input, func(t *testing.T) {
			_, err := NormalizeDomain(input)
			require.ErrorIs(t, err, ErrInvalidDomain)
		})
	}
}

func TestUIAccessDomainRequestHost(t *testing.T) {
	for _, host := range []string{"example.com", "EXAMPLE.COM.", "example.com:443", "Example.Com.:8080"} {
		require.True(t, RequestHostMatches(host, "example.com"), host)
	}
	for _, host := range []string{"", "cdn.example.com", "example.com.evil.test", "evil-example.com", "127.0.0.1", "user@example.com", "example.com..", " example.com", "example.com:80:90"} {
		require.False(t, RequestHostMatches(host, "example.com"), host)
	}
}
