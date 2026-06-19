package e2e

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jianvideo/internal/db/models"
)

// TestE2E_CreateLibraryPath 验证创建媒体库路径成功并返回 201。
func TestE2E_CreateLibraryPath(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL)

	// 创建测试目录
	dirPath := filepath.Join(tmpDir, "test_lib")
	require.NoError(t, os.MkdirAll(dirPath, 0o755))
	escapedPath := strings.ReplaceAll(dirPath, `\`, `\\`)
	body := fmt.Sprintf(`{"path":"%s","type":"local","label":"测试库"}`, escapedPath)

	resp := doRequest(t, "POST", server.URL+"/api/library/paths", body,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusCreated, resp.StatusCode, "创建路径应返回 201")

	var result models.LibraryPath
	doJSONRequest(t, resp, &result)
	assert.NotZero(t, result.ID, "返回的路径应有 ID")
	assert.Equal(t, "local", result.Type, "类型应为 local")
	assert.Equal(t, "测试库", result.Label, "标签应为 测试库")
}

// TestE2E_ListLibraryPaths 验证创建路径后能查询到。
func TestE2E_ListLibraryPaths(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL)

	// 先创建一个路径
	dirPath := filepath.Join(tmpDir, "movies_list")
	require.NoError(t, os.MkdirAll(dirPath, 0o755))
	escapedPath := strings.ReplaceAll(dirPath, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"电影库"}`, escapedPath)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	// 查询列表
	resp := doRequest(t, "GET", server.URL+"/api/library/paths", nil,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusOK, resp.StatusCode, "查询列表应返回 200")

	var result struct {
		Items []models.LibraryPath `json:"items"`
	}
	doJSONRequest(t, resp, &result)
	assert.GreaterOrEqual(t, len(result.Items), 1, "应至少有一个路径")

	// 验证包含刚创建的路径
	found := false
	for _, item := range result.Items {
		if item.Label == "电影库" {
			found = true
			break
		}
	}
	assert.True(t, found, "列表中应包含刚创建的「电影库」")
}

// TestE2E_DeleteLibraryPath 验证创建后删除成功，再次查询不再包含。
func TestE2E_DeleteLibraryPath(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL)

	// 创建路径
	dirPath := filepath.Join(tmpDir, "to_delete")
	require.NoError(t, os.MkdirAll(dirPath, 0o755))
	escapedPath := strings.ReplaceAll(dirPath, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"待删除"}`, escapedPath)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var created models.LibraryPath
	doJSONRequest(t, createResp, &created)

	// 删除
	delResp := doRequest(t, "DELETE",
		fmt.Sprintf("%s/api/library/paths/%d", server.URL, created.ID), nil,
		map[string]string{"Cookie": cookie})
	assert.Equal(t, http.StatusOK, delResp.StatusCode, "删除应返回 200")

	// 再次查询，确认已删除
	listResp := doRequest(t, "GET", server.URL+"/api/library/paths", nil,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusOK, listResp.StatusCode)

	var result struct {
		Items []models.LibraryPath `json:"items"`
	}
	doJSONRequest(t, listResp, &result)

	for _, item := range result.Items {
		assert.NotEqual(t, created.ID, item.ID, "删除后的路径不应再出现在列表中")
	}
}

// TestE2E_CreateMediaFile 验证在路径下创建媒体文件后能查询到。
func TestE2E_CreateMediaFile(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL)

	// 创建目录
	dir := filepath.Join(tmpDir, "media_test")
	os.MkdirAll(dir, 0o755)
	escapedDir := strings.ReplaceAll(dir, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"媒体测试"}`, escapedDir)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var lp models.LibraryPath
	doJSONRequest(t, createResp, &lp)

	// 创建测试视频文件
	videoPath := filepath.Join(dir, "sample.mp4")
	os.WriteFile(videoPath, []byte("fake video data for testing"), 0o644)

	// 触发扫描
	scanResp := doRequest(t, "POST",
		fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusOK, scanResp.StatusCode, "扫描应返回 200")

	// 查询媒体文件列表
	listResp := doRequest(t, "GET", server.URL+"/api/library/media", nil,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusOK, listResp.StatusCode)

	var result struct {
		Items []models.MediaFile `json:"items"`
		Total int64              `json:"total"`
	}
	doJSONRequest(t, listResp, &result)
	assert.GreaterOrEqual(t, result.Total, int64(1), "扫描后应至少有一个媒体文件")

	// 验证文件名
	found := false
	for _, item := range result.Items {
		if item.FileName == "sample.mp4" {
			found = true
			break
		}
	}
	assert.True(t, found, "媒体文件列表中应包含 sample.mp4")
}

// TestE2E_DeleteMediaFile 验证删除媒体文件成功。
func TestE2E_DeleteMediaFile(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	cookie := loginAndGetCookie(t, server.URL)

	// 创建目录并扫描
	dir := filepath.Join(tmpDir, "del_media")
	os.MkdirAll(dir, 0o755)
	escapedDir := strings.ReplaceAll(dir, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"删除测试"}`, escapedDir)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var lp models.LibraryPath
	doJSONRequest(t, createResp, &lp)

	// 创建并扫描
	os.WriteFile(filepath.Join(dir, "to_delete.mp4"), []byte("data"), 0o644)
	doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})

	// 查询获取 ID
	listResp := doRequest(t, "GET", server.URL+"/api/library/media", nil,
		map[string]string{"Cookie": cookie})
	var listResult struct {
		Items []models.MediaFile `json:"items"`
	}
	doJSONRequest(t, listResp, &listResult)
	require.NotEmpty(t, listResult.Items, "应有媒体文件")

	mediaID := listResult.Items[0].ID

	// 删除
	delResp := doRequest(t, "DELETE",
		fmt.Sprintf("%s/api/library/media/%d", server.URL, mediaID), nil,
		map[string]string{"Cookie": cookie})
	assert.Equal(t, http.StatusOK, delResp.StatusCode, "删除媒体文件应返回 200")
}
