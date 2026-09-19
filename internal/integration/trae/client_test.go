package trae

import (
	"net/url"
	"strings"
	"testing"
)

func TestOfficialUsageEndpointPinned(t *testing.T) {
	if usageURL != "https://api.trae.cn/trae/api/v2/pay/ide_user_ent_usage" {
		t.Fatal(usageURL)
	}
}

func TestBuildVerificationURLKeepsRequiredLoopbackCallback(t *testing.T) {
	got := buildVerificationURL("www.trae.cn", "trace-id", "http://127.0.0.1:41890/authorize", "machine-id", "1234567890123456", "challenge")
	for _, required := range []string{
		"https://www.trae.cn/authorization?",
		"client_id=" + clientID,
		"auth_callback_url=http://127.0.0.1:41890/authorize",
		"device_id=1234567890123456",
		"code_challenge=challenge",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("verification URL missing %q: %s", required, got)
		}
	}
}

func TestParseCallbackReadsNestedAuthCodeInfo(t *testing.T) {
	values := url.Values{"authCodeInfo": {`{"AuthCode":"real-code"}`}}
	got := parseCallback(values)
	if got.err != nil || got.authCode != "real-code" {
		t.Fatalf("callback = %#v", got)
	}
}

func TestParseExchangeCredential(t *testing.T) {
	got := parseExchangeCredential([]byte(`{"Result":{"AccessToken":"access","RefreshToken":"refresh","TokenExpireAt":1893456000000}}`))
	if got.AccessToken != "access" || got.RefreshToken != "refresh" || got.ExpiresAt.IsZero() {
		t.Fatalf("credential = %#v", got)
	}
}
