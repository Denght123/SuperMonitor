package genericquota

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchValidatesAndMapsRealResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected authorization header")
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"data":{"remaining":"72.5","total":100,"reset_at":1999999999}}`)
	}))
	defer server.Close()
	client := NewClient()
	reading, err := client.Fetch(context.Background(), Credential{Endpoint: server.URL, AuthType: "bearer", Secret: "secret", ValuePath: "data.remaining", TotalPath: "data.total", ResetAtPath: "data.reset_at"})
	if err != nil {
		t.Fatal(err)
	}
	if reading.Value != 72.5 || reading.Total == nil || *reading.Total != 100 || reading.ResetAt == nil {
		t.Fatalf("unexpected reading: %+v", reading)
	}
}

func TestNormalizeRejectsRemotePlainHTTP(t *testing.T) {
	credential := Credential{Endpoint: "http://example.com/quota", AuthType: "none", ValuePath: "remaining"}
	if err := credential.Normalize(); err == nil {
		t.Fatal("expected insecure remote endpoint to be rejected")
	}
}
