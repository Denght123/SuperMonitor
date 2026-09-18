package tokenrhythm

import "testing"

func TestNormalizeCredential(t *testing.T) {
	credential, err := NormalizeCredential("phone----sess_example----sk_tr_x----rf_tr_x")
	if err != nil || credential.SessionToken != "sess_example" {
		t.Fatalf("unexpected credential: %+v err=%v", credential, err)
	}
	credential, err = NormalizeCredential("tr_ref_device=ref; tr_session=sess_token; other=x")
	if err != nil || credential.SessionToken != "sess_token" || credential.RefDevice != "ref" {
		t.Fatalf("unexpected cookie credential: %+v err=%v", credential, err)
	}
}
