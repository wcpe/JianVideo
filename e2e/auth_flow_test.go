package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_LoginSuccess 验证正确的用户名密码登录成功，并通过 Cookie 下发登录态。
// 注：登录响应 JSON 体只携带 username，token 由 Set-Cookie 头携带（HttpOnly Cookie 鉴权）。
func TestE2E_LoginSuccess(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":"admin","password":"admin"}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "登录应返回 200")

	var result map[string]string
	parseJSON(t, resp, &result)
	assert.Equal(t, "admin", result["username"], "返回的 username 应为 admin")
	assert.NotEmpty(t, resp.Header.Get("Set-Cookie"),
		"登录响应应通过 Set-Cookie 下发鉴权 token")
}

// TestE2E_LoginWrongPassword 验证错误密码登录返回 401。
func TestE2E_LoginWrongPassword(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":"admin","password":"wrong"}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "错误密码应返回 401")

	var result map[string]string
	parseJSON(t, resp, &result)
	assert.Equal(t, "INVALID_CREDENTIALS", result["code"], "错误码应为 INVALID_CREDENTIALS")
}

// TestE2E_Logout 验证登录后登出成功。
func TestE2E_Logout(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	// 先登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	require.Equal(t, http.StatusOK, loginResp.StatusCode)
	cookie := loginResp.Header.Get("Set-Cookie")
	require.NotEmpty(t, cookie, "登录响应应包含 Set-Cookie")

	// 登出
	resp := doRequest(t, "POST", server.URL+"/api/auth/logout", nil,
		map[string]string{"Cookie": cookie})
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "登出应返回 204")
}

// TestE2E_Me 验证使用 token 获取当前用户信息。
func TestE2E_Me(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	// 先登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	require.Equal(t, http.StatusOK, loginResp.StatusCode)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 获取当前用户信息
	resp := doRequest(t, "GET", server.URL+"/api/me", nil,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusOK, resp.StatusCode, "/api/me 应返回 200")

	var result map[string]string
	parseJSON(t, resp, &result)
	assert.Equal(t, "admin", result["username"], "应返回 admin 用户名")
}

// TestE2E_UnauthorizedAccess 验证未认证访问受保护路由返回 401。
// /api/me 是当前唯一受保护路由（authMW 挂在 /api 下空分组内），故用 /api/me 做断言。
func TestE2E_UnauthorizedAccess(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	// 不传 Cookie 访问受保护路由
	resp := doRequest(t, "GET", server.URL+"/api/me", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "未认证访问应返回 401")

	var result map[string]string
	parseJSON(t, resp, &result)
	assert.Equal(t, "UNAUTHORIZED", result["code"], "错误码应为 UNAUTHORIZED")

	// 使用无效 token
	resp2 := doRequest(t, "GET", server.URL+"/api/me", nil,
		map[string]string{"Authorization": "Bearer invalid-token"})
	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode, "无效 token 应返回 401")
}

// TestE2E_LoginInvalidInput 验证空用户名登录返回 400。
func TestE2E_LoginInvalidInput(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":""}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "空用户名应返回 400")

	var result map[string]string
	parseJSON(t, resp, &result)
	assert.Equal(t, "INVALID_INPUT", result["code"], "错误码应为 INVALID_INPUT")
}

// TestE2E_LoginNonExistentUser 验证不存在的用户登录返回 401。
func TestE2E_LoginNonExistentUser(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":"nonexistent","password":"password123"}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "不存在的用户应返回 401")
}

// loginAndGetCookie 是辅助函数：登录并返回 Cookie 字符串。
func loginAndGetCookie(t *testing.T, serverURL string) string {
	t.Helper()
	loginResp := doRequest(t, "POST", serverURL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	if loginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResp.Body)
		t.Fatalf("登录失败: %d, body: %s", loginResp.StatusCode, string(body))
	}
	cookie := loginResp.Header.Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("登录响应缺少 Set-Cookie")
	}
	return cookie
}

// doJSONRequest 是辅助函数：解析 JSON 响应到 target，并在失败时给出有意义的错误信息。
func doJSONRequest(t *testing.T, resp *http.Response, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("解析 JSON 失败: %v, body: %s", err, string(body))
	}
}
