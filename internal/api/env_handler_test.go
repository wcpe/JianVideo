package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// envItem 与 SystemEnv 响应中每项的结构对应，用于测试反序列化。
type envItem struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
	Set         bool   `json:"set"`
	Value       string `json:"value"`
}

// callSystemEnv 调 SystemEnv 端点并解析返回的环境变量项列表。
func callSystemEnv(t *testing.T) []envItem {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/env", nil)
	h.SystemEnv(c)
	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp struct {
		Env []envItem `json:"env"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	return resp.Env
}

func findEnvItem(items []envItem, key string) (envItem, bool) {
	for _, it := range items {
		if it.Key == key {
			return it, true
		}
	}
	return envItem{}, false
}

// TestSystemEnv_ReturnsKnownKeys 端点应返回项目已知的全部环境变量键。
func TestSystemEnv_ReturnsKnownKeys(t *testing.T) {
	items := callSystemEnv(t)
	for _, key := range []string{
		"JIANVIDEO_FFMPEG_PATH", "JIANVIDEO_FFPROBE_PATH", "JIANVIDEO_DB_PATH",
		"JIANVIDEO_SERVER_PORT", "JIANVIDEO_MAGICK_PATH", "JIANVIDEO_DEBUG",
		"JWT_SECRET", "SMB_MASTER_PASSWORD",
	} {
		if _, ok := findEnvItem(items, key); !ok {
			t.Errorf("响应应包含已知环境变量键 %q", key)
		}
	}
}

// TestSystemEnv_SensitiveNeverLeaksPlaintext 安全红线：
// 即便敏感环境变量（JWT_SECRET / SMB_MASTER_PASSWORD）设了真实值，
// 响应里也绝不能出现明文——value 必须是固定掩码、不等于真实值，且整段响应体不含该 secret。
func TestSystemEnv_SensitiveNeverLeaksPlaintext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const jwtSecret = "super-secret-jwt-value-1234567890"
	const smbSecret = "smb-master-pass-abcdef"
	t.Setenv("JWT_SECRET", jwtSecret)
	t.Setenv("SMB_MASTER_PASSWORD", smbSecret)

	h := NewHandler(nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/env", nil)
	h.SystemEnv(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}

	// 整段响应体绝不能包含任一 secret 明文
	body := w.Body.String()
	for _, secret := range []string{jwtSecret, smbSecret} {
		if strings.Contains(body, secret) {
			t.Fatalf("响应体泄露了敏感明文 %q（安全红线）", secret)
		}
	}

	var resp struct {
		Env []envItem `json:"env"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}

	for _, tc := range []struct {
		key    string
		secret string
	}{
		{"JWT_SECRET", jwtSecret},
		{"SMB_MASTER_PASSWORD", smbSecret},
	} {
		item, ok := findEnvItem(resp.Env, tc.key)
		if !ok {
			t.Fatalf("响应应包含敏感键 %q", tc.key)
		}
		if !item.Sensitive {
			t.Errorf("%q 应标记为 sensitive", tc.key)
		}
		if !item.Set {
			t.Errorf("%q 已设置环境变量，set 应为 true", tc.key)
		}
		if item.Value == tc.secret {
			t.Errorf("%q 的 value 不得等于真实值（应脱敏）", tc.key)
		}
		if item.Value == "" {
			t.Errorf("%q 已设置时 value 应为固定掩码而非空串", tc.key)
		}
	}
}

// TestSystemEnv_NonSensitiveReturnsPlaintext 非敏感项设置后应返回明文值。
func TestSystemEnv_NonSensitiveReturnsPlaintext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("JIANVIDEO_DEBUG", "1")

	items := callSystemEnv(t)
	item, ok := findEnvItem(items, "JIANVIDEO_DEBUG")
	if !ok {
		t.Fatalf("响应应包含 JIANVIDEO_DEBUG")
	}
	if item.Sensitive {
		t.Errorf("JIANVIDEO_DEBUG 不应标记为 sensitive")
	}
	if !item.Set {
		t.Errorf("已设置 JIANVIDEO_DEBUG，set 应为 true")
	}
	if item.Value != "1" {
		t.Errorf("非敏感项应返回明文 1，实际 %q", item.Value)
	}
}
