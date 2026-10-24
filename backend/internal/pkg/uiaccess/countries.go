package uiaccess

import (
	"errors"
	"strings"
)

var ErrInvalidCountryCodes = errors.New("UI blocked countries must be a comma-separated list of official ISO 3166-1 alpha-2 codes")

// Officially assigned ISO 3166-1 alpha-2 country and territory codes. Reserved,
// withdrawn and user-assigned codes (including XX, ZZ and XK) are not accepted.
// Kept explicit because CLDR region parsers also accept non-ISO region codes.
const countryCodes = "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ " +
	"BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ " +
	"CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ " +
	"DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR " +
	"GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY " +
	"HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP " +
	"KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY " +
	"MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ " +
	"NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY " +
	"QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ " +
	"TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ " +
	"VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW"

var officialCountryCodes = func() map[string]struct{} {
	codes := strings.Fields(countryCodes)
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	return set
}()

// ValidCountryCode accepts a canonical uppercase ISO 3166-1 alpha-2 code only.
func ValidCountryCode(code string) bool {
	_, valid := officialCountryCodes[code]
	return valid
}

// NormalizeCountryCodes normalizes a CSV list and keeps each country's first
// position. An empty list is allowed here; the enabled setting validates that
// at least one country exists. Empty CSV entries are never silently discarded.
func NormalizeCountryCodes(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	values := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		code := strings.ToUpper(strings.TrimSpace(value))
		if !ValidCountryCode(code) {
			return "", ErrInvalidCountryCodes
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		normalized = append(normalized, code)
	}
	return strings.Join(normalized, ","), nil
}
