package payment

import "testing"

func TestSanitizePaymentURL(t *testing.T) {
	for _, value := range []string{
		"https://pay.example.com/order/42?return=https%3A%2F%2Fapp.example.com",
		"http://pay.example.com/order/42", "weixin://wxpay/bizpayurl?pr=42",
		"wxp://payment-token", "alipays://platformapi/startapp?saId=10000007",
		"alipay://platformapi/startapp?saId=10000007",
	} {
		t.Run(value, func(t *testing.T) {
			if got := SanitizeURL("  " + value + "  "); got != value {
				t.Fatalf("SanitizeURL() = %q, want %q", got, value)
			}
		})
	}
	for _, value := range []string{
		"javascript:alert(document.domain)", "JaVaScRiPt:alert(1)",
		"java\tscript:alert(1)", "java\nscript:alert(1)", "java\x00script:alert(1)",
		"data:text/html,<script>alert(1)</script>", "vbscript:msgbox(1)",
		"file:///C:/Windows/system.ini", "blob:https://app.example.com/42",
		"intent://pay/#Intent;scheme=javascript;end", "//attacker.example.com/pay",
		"/payment?payee=attacker", "not a url", "https:", "",
	} {
		t.Run(value, func(t *testing.T) {
			if got := SanitizeURL(value); got != "" {
				t.Fatalf("SanitizeURL() = %q for unsafe value", got)
			}
		})
	}
}
