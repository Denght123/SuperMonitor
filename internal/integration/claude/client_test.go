package claude

import "testing"

func TestParseCredential(t *testing.T) {
	credential, err := ParseCredential([]byte(`{"claudeAiOauth":{"accessToken":"token","refreshToken":"refresh","subscriptionType":"claude_pro","expiresAt":9999999999999}}`))
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "token" || credential.RefreshToken != "refresh" || credential.SubscriptionType != "claude_pro" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestParseUsage(t *testing.T) {
	usage, err := parseUsage([]byte(`{"five_hour":{"utilization":25,"resets_at":"2026-09-20T00:00:00Z"},"seven_day":{"utilization":70,"resets_at":"2026-09-25T00:00:00Z"}}`), "claude_pro")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan != "Pro" || len(usage.Windows) != 2 || usage.Windows[0].RemainingPercent != 75 || usage.Windows[1].RemainingPercent != 30 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
