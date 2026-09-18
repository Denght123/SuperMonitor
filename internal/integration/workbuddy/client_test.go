package workbuddy

import (
	"testing"
	"time"
)

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
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	resources := normalizeResources([]map[string]any{{"CycleCapacitySizePrecise": "100.5", "CycleCapacityRemainPrecise": "75.25"}, {"CapacitySize": 50.0, "CapacityRemain": 20.0}}, now)
	result := summarizeResources(resources)
	if result.Total != 150.5 || result.Remaining != 95.25 {
		t.Fatalf("unexpected credits: %+v", result)
	}
}

func TestNormalizeResourceDerivesMissingValues(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	resource := normalizeResource(map[string]any{"PackageCode": "activity", "CycleCapacityRemain": "70", "CycleCapacityUsed": "30"}, now)
	if resource.Total != 100 || resource.Remaining != 70 || resource.Used != 30 {
		t.Fatalf("unexpected resource: %+v", resource)
	}
}

func TestResourceListSupportsOfficialShapes(t *testing.T) {
	for _, payload := range []map[string]any{
		{"data": map[string]any{"Accounts": []any{map[string]any{"PackageCode": "a"}}}},
		{"data": map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"PackageCode": "b"}}}}},
		{"data": map[string]any{"Response": map[string]any{"Data": map[string]any{"Packages": []any{map[string]any{"PackageCode": "c"}}}}}},
	} {
		field := "Accounts"
		if _, ok := resourceList(payload, field); !ok {
			field = "Packages"
		}
		items, ok := resourceList(payload, field)
		if !ok || len(items) != 1 {
			t.Fatalf("failed to parse payload: %#v", payload)
		}
	}
}

func TestMergeResourcesPrefersPaidAndFreeDetails(t *testing.T) {
	summary := []CreditResource{{Code: "activity", Remaining: 90}, {Code: "summary-only", Remaining: 25}}
	details := []CreditResource{{Code: "activity", Remaining: 40}}
	merged := mergeResources(summary, details)
	if len(merged) != 2 || merged[0].Remaining != 40 || merged[1].Code != "summary-only" {
		t.Fatalf("unexpected merged resources: %+v", merged)
	}
}

func TestResolveExpiryPrefersCycleOverFarPlaceholder(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	resource := map[string]any{"DeductionEndTime": "2049-12-31 23:59:59", "CycleEndTime": "2026-09-30 23:59:59"}
	expires := resolveExpiry(resource, now)
	if expires == nil || expires.Local().Year() != 2026 || expires.Local().Month() != time.September || expires.Local().Day() != 30 {
		t.Fatalf("unexpected expiry: %v", expires)
	}
}

func TestResolveExpiryDropsFarFuturePlaceholder(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	if expires := resolveExpiry(map[string]any{"DeductionEndTime": "2049-12-31 23:59:59"}, now); expires != nil {
		t.Fatalf("expected placeholder expiry to be omitted, got %v", expires)
	}
}

func TestPackageCodeTablesContainKnownInternationalCodes(t *testing.T) {
	contains := func(values []string, expected string) bool {
		for _, value := range values {
			if value == expected {
				return true
			}
		}
		return false
	}
	for _, code := range []string{"TCACA_code_003_FAnt7lcmRT", "TCACA_code_036_lupO5WgNdG"} {
		if !contains(paidPackageCodes, code) {
			t.Fatalf("missing paid package %s", code)
		}
	}
	for _, code := range []string{"TCACA_code_001_PqouKr6QWV", "TCACA_code_035_ArVxJcGDsm", "TCACA_code_040_mi9rCYg46x"} {
		if !contains(freePackageCodes, code) {
			t.Fatalf("missing free package %s", code)
		}
	}
}

func TestWAFCodeIsNotTreatedAsExpiredToken(t *testing.T) {
	if isUnauthorized(200, map[string]any{"code": 10085, "msg": "请求不合法"}) {
		t.Fatal("WAF code 10085 must not trigger token refresh")
	}
}
