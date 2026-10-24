package uiaccess

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidCountryCode(t *testing.T) {
	require.Len(t, officialCountryCodes, 249)
	for _, code := range []string{"CN", "US", "GB", "HK", "MO", "TW", "AX", "BQ", "CW", "SX", "SS", "AQ", "PS"} {
		require.True(t, ValidCountryCode(code), code)
	}
	for _, code := range []string{"", "cn", " CN", "US ", "USA", "840", "XX", "ZZ", "XK", "AC", "CP", "DG", "EA", "EU", "EZ", "FX", "IC", "SU", "TA", "UK", "UN", "AN", "BU", "CS", "NT", "TP", "YU", "ZR", "ZZZ", "ＣＮ"} {
		require.False(t, ValidCountryCode(code), code)
	}
}

func TestNormalizeCountryCodes(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"", ""}, {" \n\t", ""}, {" cn, us, CN ,hk", "CN,US,HK"},
		{"us,cn,US", "US,CN"}, {"tw,mo,hk,gb", "TW,MO,HK,GB"},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := NormalizeCountryCodes(test.input)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
	for _, input := range []string{"CN,,US", "CN,", ",US", ",", "CN, ,US", "CN US", "CN;US", "CN，US", "USA", "840", "XX", "ZZ", "XK", "UK", "AN", "ＣＮ"} {
		t.Run(input, func(t *testing.T) {
			_, err := NormalizeCountryCodes(input)
			require.ErrorIs(t, err, ErrInvalidCountryCodes)
		})
	}
	got, err := NormalizeCountryCodes(strings.Join(strings.Fields(countryCodes), ","))
	require.NoError(t, err)
	require.Len(t, strings.Split(got, ","), 249)
}
