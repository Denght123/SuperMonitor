//go:build !windows

package netutil

import (
	"net/http"
	"net/url"
)

func platformProxy(_ *http.Request) (*url.URL, error) {
	return nil, nil
}
