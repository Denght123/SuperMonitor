package aliyunbss

import "testing"

func TestAliyunOfficialSignatureVector(t *testing.T) {
	params := map[string]string{
		"AccessKeyId": "testid", "Action": "DescribeDedicatedHosts", "Format": "JSON",
		"RegionId": "cn-beijing", "SignatureMethod": "HMAC-SHA1",
		"SignatureNonce": "edb2b34af0af9a6d14deaf7c1a5315eb", "SignatureVersion": "1.0",
		"Timestamp": "2023-03-13T08:34:30Z", "Version": "2014-05-26",
	}
	if got := sign(params, "testsecret"); got != "9NaGiOspFP5UPcwX8Iwt2YJXXuk=" {
		t.Fatalf("signature = %s", got)
	}
}

func TestOfficialEndpointPinned(t *testing.T) {
	if endpoint != "https://business.aliyuncs.com/" {
		t.Fatal(endpoint)
	}
}
