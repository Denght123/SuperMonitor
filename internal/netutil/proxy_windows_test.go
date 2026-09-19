//go:build windows

package netutil

import "testing"

func TestGlobalAIHostClassification(t *testing.T) {
	for _, host := range []string{"chatgpt.com", "auth.openai.com", "api2.cursor.sh", "q.us-east-1.amazonaws.com"} {
		if !isGlobalAIHost(host) {
			t.Errorf("expected %s to use loopback proxy fallback", host)
		}
	}
	for _, host := range []string{"api.trae.cn", "open.bigmodel.cn", "business.aliyuncs.com"} {
		if isGlobalAIHost(host) {
			t.Errorf("domestic host %s must stay direct when ProxyEnable=0", host)
		}
	}
}
