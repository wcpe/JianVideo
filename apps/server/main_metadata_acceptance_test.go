package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/auth"
)

const metadataAcceptanceSecret = "fr2-030-acceptance-secret"

func TestSingleBinaryScansAndServesEmbeddedMetadata(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("未安装 ffmpeg，跳过单二进制真实素材验收")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("未安装 ffprobe，跳过单二进制真实素材验收")
	}

	tempDir := t.TempDir()
	mediaDir := filepath.Join(tempDir, "media")
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		t.Fatalf("创建验收媒体目录失败: %v", err)
	}
	mediaPath := filepath.Join(mediaDir, "embedded-metadata.mp4")
	generateMetadataAcceptanceVideo(t, ffmpegPath, mediaPath)

	dbPath := filepath.Join(tempDir, "jianvideo-fr2-030.sqlite")
	binaryPath := buildJianVideoBinary(t, tempDir)
	port := reserveTCPPort(t)
	command, logFile, logPath := newMetadataAcceptanceCommand(t, binaryPath, dbPath, tempDir, port, ffmpegPath, ffprobePath)
	waitDone := startAcceptanceCommand(t, command, logFile)
	waitForBinaryStartup(t, port, waitDone, logPath)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	setupMetadataAcceptanceUser(t, baseURL)
	token, err := auth.GenerateToken("admin", metadataAcceptanceSecret, time.Hour)
	if err != nil {
		t.Fatalf("生成验收令牌失败: %v", err)
	}
	libraryID := createMetadataAcceptanceLibrary(t, baseURL, token, mediaDir)
	triggerMetadataAcceptanceScan(t, baseURL, token, libraryID)
	mediaID := waitForMetadataAcceptanceMedia(t, dbPath, mediaPath)
	waitForMetadataAcceptanceAPI(t, baseURL, token, mediaID)
	waitForMetadataAcceptanceScanCompletion(t, dbPath, libraryID)
}

func generateMetadataAcceptanceVideo(t *testing.T, ffmpegPath, outputPath string) {
	t.Helper()
	args := []string{
		"-y", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=24:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-map", "0:v", "-map", "1:a", "-c:v", "mpeg4", "-c:a", "aac",
		"-metadata", "title=FR2-030 单二进制验收", "-metadata:s:a:0", "language=zh", outputPath,
	}
	if output, err := exec.Command(ffmpegPath, args...).CombinedOutput(); err != nil {
		t.Fatalf("生成单二进制验收视频失败: %v\n%s", err, output)
	}
}

func newMetadataAcceptanceCommand(t *testing.T, binaryPath, dbPath, tempDir string, port int, ffmpegPath, ffprobePath string) (*exec.Cmd, *os.File, string) {
	t.Helper()
	logPath := filepath.Join(tempDir, "metadata-startup.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("创建元数据验收日志失败: %v", err)
	}
	command := exec.Command(binaryPath)
	command.Env = append(os.Environ(),
		"DB_PATH="+dbPath,
		"SERVER_PORT="+strconv.Itoa(port),
		"JWT_SECRET="+metadataAcceptanceSecret,
		"JIANVIDEO_FFMPEG_PATH="+ffmpegPath,
		"JIANVIDEO_FFPROBE_PATH="+ffprobePath,
		"JIANVIDEO_MAGICK_PATH="+filepath.Join(tempDir, "missing-magick"),
	)
	command.Stdout, command.Stderr = logFile, logFile
	return command, logFile, logPath
}

func setupMetadataAcceptanceUser(t *testing.T, baseURL string) {
	t.Helper()
	response := metadataAcceptanceRequest(t, http.MethodPost, baseURL+"/api/auth/setup", "", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("单二进制首次初始化失败: status=%d", response.StatusCode)
	}
}

func createMetadataAcceptanceLibrary(t *testing.T, baseURL, token, mediaDir string) int64 {
	t.Helper()
	response := metadataAcceptanceRequest(t, http.MethodPost, baseURL+"/api/library/paths", token, map[string]any{
		"path":         mediaDir,
		"type":         "local",
		"label":        "FR2-030 验收库",
		"library_kind": "mixed",
	})
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("创建单二进制验收媒体库失败: status=%d", response.StatusCode)
	}
	var library struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&library); err != nil || library.ID <= 0 {
		t.Fatalf("解析验收媒体库响应失败: id=%d err=%v", library.ID, err)
	}
	return library.ID
}

func triggerMetadataAcceptanceScan(t *testing.T, baseURL, token string, libraryID int64) {
	t.Helper()
	response := metadataAcceptanceRequest(t, http.MethodPost, fmt.Sprintf("%s/api/library/scan/%d", baseURL, libraryID), token, nil)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		t.Fatalf("触发单二进制验收扫描失败: status=%d", response.StatusCode)
	}
}

func waitForMetadataAcceptanceMedia(t *testing.T, dbPath, mediaPath string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=10000")
	if err != nil {
		t.Fatalf("打开单二进制验收数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var mediaID int64
		err := db.QueryRow("SELECT id FROM media_files WHERE file_name = ?", filepath.Base(mediaPath)).Scan(&mediaID)
		if err == nil {
			return mediaID
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("单二进制扫描未在期限内完成媒体入库")
	return 0
}

func waitForMetadataAcceptanceScanCompletion(t *testing.T, dbPath string, libraryID int64) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=10000")
	if err != nil {
		t.Fatalf("打开单二进制验收数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := db.QueryRow("SELECT status FROM scan_tasks WHERE library_id = ? ORDER BY id DESC LIMIT 1", libraryID).Scan(&status)
		if err == nil && status == "completed" {
			return
		}
		if err == nil && (status == "failed" || status == "canceled") {
			t.Fatalf("单二进制扫描以异常终态结束: %s", status)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("单二进制扫描任务未在期限内完成")
}

func waitForMetadataAcceptanceAPI(t *testing.T, baseURL, token string, mediaID int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response := metadataAcceptanceRequest(t, http.MethodGet, fmt.Sprintf("%s/api/library/media/%d/metadata", baseURL, mediaID), token, nil)
		var payload struct {
			Items []struct {
				Source         string `json:"source"`
				ToolVersion    string `json:"tool_version"`
				NormalizedJSON string `json:"normalized_json"`
				Stale          bool   `json:"stale"`
			} `json:"items"`
		}
		err := json.NewDecoder(response.Body).Decode(&payload)
		_ = response.Body.Close()
		if response.StatusCode == http.StatusOK && err == nil && len(payload.Items) == 1 {
			item := payload.Items[0]
			if item.Source != "ffprobe" || item.ToolVersion == "" || item.Stale {
				t.Fatalf("单二进制元数据来源或状态错误: %+v", item)
			}
			var normalized struct {
				VideoStreams []json.RawMessage `json:"video_streams"`
				AudioStreams []json.RawMessage `json:"audio_streams"`
			}
			if err := json.Unmarshal([]byte(item.NormalizedJSON), &normalized); err != nil {
				t.Fatalf("单二进制规范化元数据 JSON 无效: %v", err)
			}
			if len(normalized.VideoStreams) != 1 || len(normalized.AudioStreams) != 1 {
				t.Fatalf("单二进制元数据流数量错误: video=%d audio=%d", len(normalized.VideoStreams), len(normalized.AudioStreams))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("单二进制未在期限内通过 API 返回解析后的文件元数据")
}

func metadataAcceptanceRequest(t *testing.T, method, requestURL, token string, body any) *http.Response {
	t.Helper()
	var requestBody bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&requestBody).Encode(body); err != nil {
			t.Fatalf("编码单二进制验收请求失败: %v", err)
		}
	}
	request, err := http.NewRequest(method, requestURL, &requestBody)
	if err != nil {
		t.Fatalf("创建单二进制验收请求失败: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("执行单二进制验收请求失败: %v", err)
	}
	return response
}
