package zhipu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseUsageRecognizesCreditWindows(t *testing.T) {
	usage, err := parseUsage([]byte(`{"code":200,"success":true,"data":{"level":"lite","limits":[{"type":"CREDIT_LIMIT","unit":3,"usage":2000,"currentValue":0,"remaining":2000,"percentage":0},{"type":"CREDIT_LIMIT","unit":6,"usage":10000,"currentValue":4788,"remaining":5212,"percentage":47,"nextResetTime":1787919529998}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan != "lite" || len(usage.Windows) != 2 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if usage.Windows[0].Label != "5 小时限额" || usage.Windows[0].RemainingPercent != 100 {
		t.Fatalf("unexpected five-hour window: %+v", usage.Windows[0])
	}
	if !usage.Windows[0].HasRemainingPercent {
		t.Fatal("coding plan window must expose a remaining percentage")
	}
	if usage.Windows[1].Label != "周限额" || usage.Windows[1].Remaining != 5212 || usage.Windows[1].ResetAt == nil {
		t.Fatalf("unexpected weekly window: %+v", usage.Windows[1])
	}
}

func TestParseUsageRejectsBusiness401(t *testing.T) {
	if _, err := parseUsage([]byte(`{"code":401,"msg":"令牌已过期或验证不正确","success":false}`)); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestNoCodingPlanIsNotTreatedAsInvalidAPIKey(t *testing.T) {
	for _, message := range []string{
		"智谱额度接口返回业务错误: 当前用户不存在 coding plan",
		"智谱额度接口返回业务错误: 当前用户不存在coding plan",
		"NO_CODING_PLAN",
	} {
		if !isNoCodingPlan(fmt.Errorf("%s", message)) {
			t.Fatalf("expected %q to use the ordinary API fallback", message)
		}
	}
	if isNoCodingPlan(fmt.Errorf("智谱 API Key 已失效")) {
		t.Fatal("invalid API key must not be classified as a missing Coding Plan")
	}
}

func TestParseAccountBalance(t *testing.T) {
	usage, err := parseAccountBalance([]byte(`{"code":200,"success":true,"data":{"availableBalance":"18.75","rechargeAmount":"100"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan != "开放平台按量计费" || len(usage.Windows) != 1 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	window := usage.Windows[0]
	if window.Kind != "balance" || window.Unit != "CNY" || window.Remaining != 18.75 {
		t.Fatalf("unexpected balance window: %+v", window)
	}
	if window.HasRemainingPercent {
		t.Fatal("a monetary balance must not be rendered as a percentage quota")
	}
}

func TestFetchUsageFallsBackFromMissingCodingPlanToBalance(t *testing.T) {
	var balanceAuthorization string
	mux := http.NewServeMux()
	mux.HandleFunc("/quota", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":500,"success":false,"msg":"当前用户不存在coding plan"}`))
	})
	mux.HandleFunc("/balance", func(w http.ResponseWriter, r *http.Request) {
		balanceAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"code":200,"success":true,"data":{"availableBalance":21.5}}`))
	})
	mux.HandleFunc("/models", func(http.ResponseWriter, *http.Request) {
		t.Fatal("model validation must not run when a real balance is available")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{
		http:             server.Client(),
		quotaURL:         server.URL + "/quota",
		accountReportURL: server.URL + "/balance",
		modelsURL:        server.URL + "/models",
	}
	usage, err := client.FetchUsage(context.Background(), Credential{APIKey: "valid-key"})
	if err != nil {
		t.Fatal(err)
	}
	if balanceAuthorization != "Bearer valid-key" {
		t.Fatalf("unexpected balance authorization: %q", balanceAuthorization)
	}
	if len(usage.Windows) != 1 || usage.Windows[0].Remaining != 21.5 {
		t.Fatalf("unexpected fallback usage: %+v", usage)
	}
}

func TestFetchUsageValidatesModelsWhenBalanceIsUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/quota", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":500,"success":false,"msg":"当前用户不存在 coding plan"}`))
	})
	mux.HandleFunc("/balance", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":1001,"success":false,"msg":"余额接口暂不可用"}`))
	})
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer valid-key" {
			t.Fatalf("unexpected models authorization: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-4.5"},{"id":"glm-4-flash"}]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{
		http:             server.Client(),
		quotaURL:         server.URL + "/quota",
		accountReportURL: server.URL + "/balance",
		modelsURL:        server.URL + "/models",
	}
	usage, err := client.FetchUsage(context.Background(), Credential{APIKey: "valid-key"})
	if err != nil {
		t.Fatal(err)
	}
	if usage.ModelCount != 2 || usage.Plan != "开放平台 API Key · 2 个可用模型" || len(usage.Windows) != 0 {
		t.Fatalf("unexpected models fallback: %+v", usage)
	}
}
