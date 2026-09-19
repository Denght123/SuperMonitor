package sqlite

import "testing"

func TestAllProductionAdaptersAreConnectable(t *testing.T) {
	for _, providerID := range []string{
		"codex", "workbuddy-cn", "workbuddy-global", "trae-cn", "qoder-cn", "qoder-global",
		"coze-cn", "bailian", "kiro", "cursor", "deepseek", "mimo", "tokenrhythm",
		"zhipu", "gemini-cli", "claude-code",
	} {
		_, _, connectable := providerPresentation(providerID)
		if !connectable {
			t.Errorf("provider %s has a production adapter but liveAuth=false", providerID)
		}
	}
}
