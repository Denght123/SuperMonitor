package gemini

import "testing"

func TestParseCredential(t *testing.T) {
	credential, err := ParseCredential([]byte(`{"access_token":"at","refresh_token":"rt","expiry_date":9999999999999}`))
	if err != nil || credential.AccessToken != "at" || credential.RefreshToken != "rt" {
		t.Fatalf("unexpected credential: %+v err=%v", credential, err)
	}
}

func TestParseUsageGroupsModels(t *testing.T) {
	usage, _, err := parseUsage([]byte(`{"buckets":[{"modelId":"gemini-2.5-flash","remainingFraction":0.75,"resetTime":"2026-09-20T00:00:00Z"},{"modelId":"gemini-2.5-pro","remainingFraction":0.4},{"modelId":"gemini-2.5-flash-lite","remainingFraction":0.9}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(usage.Windows) != 3 || usage.Windows[0].Label != "Pro 模型额度" || usage.Windows[0].RemainingPercent != 40 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
