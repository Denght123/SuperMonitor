package netutil

import (
	"net/http"
	"time"
)

// NewHTTPClient builds the shared outbound client used by provider integrations.
// Environment proxy variables take precedence; Windows additionally falls back
// to the current user's Internet Settings proxy so the service follows the same
// route as the browser and PowerShell.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyFunc()
	transport.ForceAttemptHTTP2 = true
	return &http.Client{Timeout: timeout, Transport: transport}
}
