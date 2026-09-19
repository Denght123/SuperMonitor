package coze

import "testing"

func TestOfficialEndpointsPinned(t *testing.T) {
	if userInfoURL != "https://www.coze.cn/api/user/info" || balanceURL != "https://www.coze.cn/api/marketplace/trade/credit/balance" {
		t.Fatal("Coze endpoints changed")
	}
}

func TestFindNestedBalance(t *testing.T) {
	value, ok := findNumber(map[string]any{"data": map[string]any{"credit_balance": "1200"}}, "credit_balance", "balance")
	if !ok || value != 1200 {
		t.Fatalf("%v %v", value, ok)
	}
}
