//go:build windows

package netutil

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func proxyFunc() func(*http.Request) (*url.URL, error) {
	system := windowsInternetProxy()
	return func(request *http.Request) (*url.URL, error) {
		configured, err := http.ProxyFromEnvironment(request)
		if err != nil || configured != nil {
			return configured, err
		}
		if system == nil || request == nil || request.URL == nil || shouldBypassProxy(request.URL.Hostname(), system.bypass) {
			return nil, nil
		}
		if request.URL.Scheme == "https" && system.https != nil {
			return system.https, nil
		}
		return system.http, nil
	}
}

type internetProxy struct {
	http, https *url.URL
	bypass      []string
}

func windowsInternetProxy() *internetProxy {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return nil
	}
	proxy := &internetProxy{}
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
