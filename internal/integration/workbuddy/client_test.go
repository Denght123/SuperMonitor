package workbuddy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

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

func TestDedupeResourcesOnlyRemovesExactDuplicates(t *testing.T) {
	expiryA := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	expiryB := expiryA.Add(time.Second)
	resources := []CreditResource{
		{Code: "bonus", Name: "拉新权益包", Total: 6, Remaining: 6, ExpiresAt: &expiryA},
		{Code: "bonus", Name: "拉新权益包", Total: 6, Remaining: 6, ExpiresAt: &expiryA},
		{Code: "bonus", Name: "拉新权益包", Total: 6, Remaining: 6, ExpiresAt: &expiryB},
		{Code: "bonus", Name: "拉新权益包", Total: 66, Remaining: 66, ExpiresAt: &expiryA},
	}
	result := dedupeResources(resources)
	if len(result) != 3 {
		t.Fatalf("expected only exact duplicates to be removed, got %+v", result)
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

func TestCheckinStatusRequiresExplicitBooleanField(t *testing.T) {
	if _, found := firstBool(map[string]any{}, "today_checked_in", "todayCheckedIn"); found {
		t.Fatal("missing check-in status must not be interpreted as not checked in")
	}
	if _, found := firstBool(map[string]any{"todayCheckedIn": "false"}, "today_checked_in", "todayCheckedIn"); found {
		t.Fatal("non-boolean check-in status must not be accepted")
	}
	checked, found := firstBool(map[string]any{"todayCheckedIn": false}, "today_checked_in", "todayCheckedIn")
	if !found || checked {
		t.Fatal("explicit false check-in status should be recognized")
	}
}

func TestCheckinSuccessRequiresExplicitBusinessSignal(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"code zero":           {"code": float64(0)},
		"code 200":            {"code": float64(200)},
		"success true":        {"success": true},
		"ok true":             {"ok": true},
		"checked status true": {"data": map[string]any{"todayCheckedIn": true}},
	} {
		t.Run(name, func(t *testing.T) {
			if !hasExplicitCheckinSuccess(payload) {
				t.Fatalf("expected explicit success for %#v", payload)
			}
		})
	}
	for name, payload := range map[string]map[string]any{
		"data envelope only": {"data": map[string]any{}},
		"success false":      {"success": false, "data": map[string]any{}},
		"checked false":      {"data": map[string]any{"todayCheckedIn": false}},
		"business failure":   {"code": float64(500), "message": "failed"},
		"failure beats bool": {"code": float64(500), "success": true},
	} {
		t.Run(name, func(t *testing.T) {
			if hasExplicitCheckinSuccess(payload) {
				t.Fatalf("must not accept ambiguous or failed response %#v", payload)
			}
		})
	}
}

func TestCheckinVerifiesAmbiguousSuccessResponse(t *testing.T) {
	statusCalls := 0
	dailyCalls := 0
	client := &Client{http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			statusCalls++
			checked := statusCalls > 1
			if checked {
				return jsonResponse(`{"code":0,"data":{"todayCheckedIn":true}}`), nil
			}
			return jsonResponse(`{"code":0,"data":{"todayCheckedIn":false}}`), nil
		case "/v2/billing/meter/daily-checkin":
			dailyCalls++
			return jsonResponse(`{"data":{"requestId":"accepted-without-result"}}`), nil
		default:
			t.Fatalf("unexpected request path: %s", request.URL.Path)
			return nil, nil
		}
	})}}

	already, refreshed, err := client.Checkin(context.Background(), &Credential{AccessToken: "token", Domain: "www.codebuddy.cn", Variant: VariantCN})
	if err != nil {
		t.Fatal(err)
	}
	if already || refreshed {
		t.Fatalf("unexpected flags: already=%v refreshed=%v", already, refreshed)
	}
	if statusCalls != 2 || dailyCalls != 1 {
		t.Fatalf("expected initial status, one submit, and one verification; status=%d daily=%d", statusCalls, dailyCalls)
	}
}

func TestCheckinRejectsAmbiguousResponseWhenStatusIsStillUnchecked(t *testing.T) {
	statusCalls := 0
	client := &Client{http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			statusCalls++
			return jsonResponse(`{"code":0,"data":{"todayCheckedIn":false}}`), nil
		case "/v2/billing/meter/daily-checkin":
			return jsonResponse(`{"data":{"requestId":"not-a-success-signal"}}`), nil
		default:
			t.Fatalf("unexpected request path: %s", request.URL.Path)
			return nil, nil
		}
	})}}

	_, _, err := client.Checkin(context.Background(), &Credential{AccessToken: "token", Domain: "www.codebuddy.cn", Variant: VariantCN})
	if err == nil || !strings.Contains(err.Error(), "仍显示今日未签到") {
		t.Fatalf("expected unconfirmed check-in error, got %v", err)
	}
	if statusCalls != 2 {
		t.Fatalf("expected status to be rechecked, got %d calls", statusCalls)
	}
}

func TestCheckinDoesNotRecheckExplicitBusinessSuccess(t *testing.T) {
	statusCalls := 0
	client := &Client{http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			statusCalls++
			return jsonResponse(`{"code":0,"data":{"todayCheckedIn":false}}`), nil
		case "/v2/billing/meter/daily-checkin":
			return jsonResponse(`{"code":0,"data":{"reward":10}}`), nil
		default:
			t.Fatalf("unexpected request path: %s", request.URL.Path)
			return nil, nil
		}
	})}}

	_, _, err := client.Checkin(context.Background(), &Credential{AccessToken: "token", Domain: "www.codebuddy.cn", Variant: VariantCN})
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 {
		t.Fatalf("explicit business success should not need verification, got %d status calls", statusCalls)
	}
}
