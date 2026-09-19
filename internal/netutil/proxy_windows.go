//go:build windows

package netutil

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

func platformProxy(request *http.Request) (*url.URL, error) {
	// Read on every request so Clash/V2Ray/other desktop proxy switches take
	// effect without restarting SuperMonitor.
	system := windowsInternetProxy()
	if system == nil || request == nil || request.URL == nil || shouldBypassProxy(request.URL.Hostname(), system.bypass) {
		return nil, nil
	}
	selected := system.http
	if request.URL.Scheme == "https" && system.https != nil {
		selected = system.https
	}
	if selected == nil {
		return nil, nil
	}
	// Proxy apps frequently keep their local listener and registry address but
	// disable ProxyEnable while using TUN mode. For global AI providers only,
	// reuse that reachable loopback listener so OpenAI/Cursor/Kiro requests do
	// not silently fall back to a blocked direct route. Domestic providers stay
	// direct unless Windows proxy is explicitly enabled.
	if !system.enabled {
		if !isGlobalAIHost(request.URL.Hostname()) || !isReachableLoopbackProxy(selected) {
			return nil, nil
		}
	}
	return selected, nil
}

type internetProxy struct {
	http, https *url.URL
	bypass      []string
	enabled     bool
}

func windowsInternetProxy() *internetProxy {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	enabled, _, _ := key.GetIntegerValue("ProxyEnable")
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return nil
	}
	proxy := &internetProxy{enabled: enabled != 0}
	entries := strings.Split(server, ";")
	if len(entries) == 1 && !strings.Contains(entries[0], "=") {
		proxy.http = parseProxyURL(entries[0])
		proxy.https = proxy.http
	} else {
		for _, entry := range entries {
			name, value, ok := strings.Cut(entry, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "http":
				proxy.http = parseProxyURL(value)
			case "https":
				proxy.https = parseProxyURL(value)
			}
		}
	}
	if proxy.http == nil {
		proxy.http = proxy.https
	}
	if proxy.https == nil {
		proxy.https = proxy.http
	}
	if proxy.http == nil {
		return nil
	}
	if override, _, readErr := key.GetStringValue("ProxyOverride"); readErr == nil {
		for _, item := range strings.Split(override, ";") {
			if value := strings.ToLower(strings.TrimSpace(item)); value != "" {
				proxy.bypass = append(proxy.bypass, value)
			}
		}
	}
	return proxy
}

func isReachableLoopbackProxy(proxyURL *url.URL) bool {
	if proxyURL == nil {
		return false
	}
	host := proxyURL.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return false
	}
	connection, err := net.DialTimeout("tcp", proxyURL.Host, 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func isGlobalAIHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range []string{
		"openai.com", "chatgpt.com", "anthropic.com", "claude.ai",
		"googleapis.com", "google.com", "cursor.com", "cursor.sh",
		"kiro.dev", "amazonaws.com", "qoder.sh", "trae.ai",
	} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func parseProxyURL(value string) *url.URL {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return nil
	}
	return parsed
}

func shouldBypassProxy(host string, rules []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	if host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return true
	}
	for _, rule := range rules {
		if rule == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if rule == host {
			return true
		}
		if strings.HasPrefix(rule, "*.") && (host == rule[2:] || strings.HasSuffix(host, rule[1:])) {
			return true
		}
	}
	return false
}
