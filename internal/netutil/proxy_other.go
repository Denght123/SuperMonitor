//go:build !windows

package netutil

import (
	"net/http"
	"net/url"
)

func proxyFunc() func(*http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment
}
