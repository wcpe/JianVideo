package audit

import (
	"strings"
	"testing"
)

func TestRedactJSON_RemovesSensitiveValues(t *testing.T) {
	raw := []byte(`{
		"password":"secret",
		"network_proxy":"http://user:pass@example.com:8080",
		"nested":{"api_token":"abc","path":"C:/Users/alice/video.mp4"},
		"safe":"visible"
	}`)

	got, err := RedactJSON(raw)
	if err != nil {
		t.Fatalf("脱敏失败: %v", err)
	}
	text := string(got)
	for _, leaked := range []string{"secret", "user:pass", "abc", "alice"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("脱敏结果泄露敏感内容 %q: %s", leaked, text)
		}
	}
	if !strings.Contains(text, "visible") {
		t.Fatalf("非敏感字段应保留, 实际: %s", text)
	}
}
