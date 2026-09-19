package qoder

import "testing"

func TestParseCredentialNestedCPA(t *testing.T) {
	credential, err := ParseCredential([]byte(`{"auth":{"accessToken":"dt-test","refreshToken":"drt-test"},"account":{"uid":"u1","nickname":"tester"}}`), RegionCN)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "dt-test" || credential.RefreshToken != "drt-test" || credential.Region != RegionCN {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestOfficialEndpointsStayPinned(t *testing.T) {
	if got := openAPI(RegionCN); got != "https://openapi.qoder.com.cn" {
		t.Fatal(got)
	}
	if got := openAPI(RegionGlobal); got != "https://openapi.qoder.sh" {
		t.Fatal(got)
	}
}
