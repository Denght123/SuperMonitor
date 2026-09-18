package workbuddy

import "testing"

func TestParseCredential(t *testing.T) {
	raw := []byte(`{"account":{"uid":"u-1","nickname":"测试","email":"dev@example.com"},"auth":{"accessToken":"at-1","refreshToken":"rt-1","domain":"www.codebuddy.cn"}}`)
	credential, err := ParseCredential(raw, VariantCN)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-1" || credential.UID != "u-1" || credential.Email != "dev@example.com" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestSummarizeCredits(t *testing.T) {
	result := summarize([]map[string]any{{"CycleCapacitySizePrecise": "100.5", "CycleCapacityRemainPrecise": "75.25"}, {"CapacitySize": 50.0, "CapacityRemain": 20.0}})
	if result.Total != 150.5 || result.Remaining != 95.25 {
		t.Fatalf("unexpected credits: %+v", result)
	}
}
