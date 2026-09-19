package kiro

import "testing"

func TestOfficialEndpointsPinned(t *testing.T) {
	if authPortalURL != "https://app.kiro.dev/signin" || tokenURL != "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/token" {
		t.Fatal("Kiro endpoints changed")
	}
}

func TestRegionFromARN(t *testing.T) {
	if got := regionFromARN("arn:aws:codewhisperer:eu-central-1:123:profile/x"); got != "eu-central-1" {
		t.Fatal(got)
	}
}
