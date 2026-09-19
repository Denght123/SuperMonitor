package netutil

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// NewHTTPClient builds the shared outbound client used by provider integrations.
// Environment proxy variables take precedence; Windows additionally falls back
// to the current user's Internet Settings proxy so the service follows the same
// route as the browser and PowerShell.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = dynamicProxyFunc()
	transport.ForceAttemptHTTP2 = true
	return &http.Client{Timeout: timeout, Transport: transport}
}

// dynamicProxyFunc deliberately resolves the proxy for every request. Desktop
// proxy applications are commonly enabled after SuperMonitor starts; caching
// Windows Internet Settings at process start made those changes invisible until
// the service was restarted.
func dynamicProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(request *http.Request) (*url.URL, error) {
		if configured := strings.TrimSpace(os.Getenv("SUPMON_PROXY_URL")); configured != "" {
			proxyURL, err := url.Parse(configured)
			if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
				return nil, &url.Error{Op: "parse", URL: "SUPMON_PROXY_URL", Err: errInvalidProxyURL}
			}
			return proxyURL, nil
		}
		if configured, err := http.ProxyFromEnvironment(request); err != nil || configured != nil {
			return configured, err
		}
		return platformProxy(request)
	}
}

type proxyConfigError string

func (e proxyConfigError) Error() string { return string(e) }

const errInvalidProxyURL proxyConfigError = "代理地址无效，应为 http://、https:// 或 socks5:// 地址"
