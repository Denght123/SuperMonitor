package cursor

import (
	"encoding/base64"
	"testing"
)

func TestWorkOSUserID(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_abc123"}`))
	if got := workOSUserID("x." + payload + ".y"); got != "user_abc123" {
		t.Fatal(got)
	}
}

func TestOfficialURLsStayPinned(t *testing.T) {
	if usageURL != "https://cursor.com/api/usage-summary" || pollURL != "https://api2.cursor.sh/auth/poll" {
		t.Fatal("Cursor official endpoint changed unexpectedly")
	}
}
