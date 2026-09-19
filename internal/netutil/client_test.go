package netutil

import (
	"net/http"
	"testing"
)

func TestDynamicProxyUsesSuperMonitorOverride(t *testing.T) {
	t.Setenv("SUPMON_PROXY_URL", "http://127.0.0.1:7897")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	req, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dynamicProxyFunc()(req)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.String() != "http://127.0.0.1:7897" {
		t.Fatalf("proxy = %v", got)
	}
}

func TestDynamicProxyRejectsInvalidOverride(t *testing.T) {
	t.Setenv("SUPMON_PROXY_URL", "://bad")
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if _, err := dynamicProxyFunc()(req); err == nil {
		t.Fatal("expected invalid proxy error")
	}
}
