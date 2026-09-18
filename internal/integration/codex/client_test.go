package codex

import (
	"encoding/base64"
	"fmt"
	"testing"
)

func TestParseCredentialSupportsCodexAuthJSON(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"dev@example.com","exp":1999999999,"https://api.openai.com/auth":{"chatgpt_account_id":"acct-1","chatgpt_plan_type":"plus"}}`))
	jwt := fmt.Sprintf("header.%s.signature", payload)
	raw := []byte(fmt.Sprintf(`{"tokens":{"access_token":%q,"refresh_token":"refresh","id_token":%q}}`, jwt, jwt))
	credential, err := ParseCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Email != "dev@example.com" || credential.AccountID != "acct-1" || credential.Plan != "plus" {
		t.Fatalf("unexpected credential metadata: %+v", credential)
	}
}

func TestParseUsageUsesNativeWindowDuration(t *testing.T) {
	usage, err := parseUsage([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":28,"limit_window_seconds":18000,"reset_at":1999999999},"secondary_window":{"used_percent":61,"limit_window_seconds":604800,"reset_at":1999999999}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(usage.Windows) != 2 || usage.Windows[0].Label != "5 小时限额" || usage.Windows[0].RemainingPercent != 72 || usage.Windows[1].Label != "周限额" {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestParseCredentialRejectsMissingToken(t *testing.T) {
	if _, err := ParseCredential([]byte(`{"tokens":{}}`)); err == nil {
		t.Fatal("expected missing access token error")
	}
}
