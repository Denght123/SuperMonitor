package codex

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"
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

func TestParseCredentialSupportsCPAFlatJSON(t *testing.T) {
	credential, err := ParseCredential([]byte(`{"type":"codex","access_token":"at-cpa","refresh_token":"rt-cpa","account_id":"acct-cpa","email":"cpa@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-cpa" || credential.RefreshToken != "rt-cpa" || credential.AccountID != "acct-cpa" || credential.Email != "cpa@example.com" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestParseCredentialSupportsSub2APIWrapperAndStringAuthJSON(t *testing.T) {
	raw := []byte(`{"account":{"email":"sub@example.com","chatgpt_account_id":"acct-sub"},"credentials":{"auth_json":"{\"tokens\":{\"access_token\":\"at-sub\",\"refresh_token\":\"rt-sub\"}}"}}`)
	credential, err := ParseCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-sub" || credential.RefreshToken != "rt-sub" || credential.AccountID != "acct-sub" || credential.Email != "sub@example.com" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestParseCredentialSupportsArrayAndCamelCase(t *testing.T) {
	credential, err := ParseCredential([]byte(`[{"provider":"codex","oauth":{"accessToken":"at-array","refreshToken":"rt-array","idToken":"id-array"},"accountId":"acct-array"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-array" || credential.RefreshToken != "rt-array" || credential.IDToken != "id-array" || credential.AccountID != "acct-array" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestParseCredentialAllowsRefreshOnly(t *testing.T) {
	credential, err := ParseCredential([]byte(`{"credentials":{"refresh_token":"rt-only"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "" || credential.RefreshToken != "rt-only" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestStartDeviceLoginLive(t *testing.T) {
	if os.Getenv("SUPMON_LIVE_CODEX") != "1" {
		t.Skip("set SUPMON_LIVE_CODEX=1 to exercise the official device endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	challenge, err := NewClient().StartDeviceLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.DeviceAuthID == "" || challenge.UserCode == "" || challenge.VerifyURL == "" {
		t.Fatalf("incomplete challenge: %+v", challenge)
	}
}
