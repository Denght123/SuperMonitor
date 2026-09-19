package zhipu

import (
	"fmt"
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
	err := fmt.Errorf("智谱额度接口返回业务错误: 当前用户不存在 coding plan")
	if !isNoCodingPlan(err) {
		t.Fatal("expected ordinary API key without Coding Plan to use the model-list fallback")
	}
	if isNoCodingPlan(fmt.Errorf("智谱 API Key 已失效")) {
		t.Fatal("invalid API key must not be classified as a missing Coding Plan")
	}
}
