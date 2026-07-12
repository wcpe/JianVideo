package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSingleBinaryGeneratesRealImageAndVideoThumbnails(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("需要 ffmpeg 验收真实视频缩略图")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("需要 ffprobe 扫描真实视频素材")
	}
	tempDir := t.TempDir()
	mediaDir := filepath.Join(tempDir, "media")
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		t.Fatalf("创建真实素材目录失败: %v", err)
	}
	writeRealImage(t, filepath.Join(mediaDir, "acceptance-image.jpg"))
	writeRealVideo(t, ffmpegPath, filepath.Join(mediaDir, "acceptance-video.mp4"))

	dbPath := filepath.Join(tempDir, "jianvideo.sqlite")
	binaryPath := buildJianVideoBinary(t, tempDir)
	port := reserveTCPPort(t)
	command, logFile, logPath := newThumbnailAcceptanceCommand(t, binaryPath, dbPath, tempDir, port, ffmpegPath, ffprobePath)
	waitDone := startAcceptanceCommand(t, command, logFile)
	waitForBinaryStartup(t, port, waitDone, logPath)

	client := newAcceptanceHTTPClient(t)
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	setupAndLogin(t, client, baseURL)
	libraryID := createAcceptanceLibrary(t, client, baseURL, mediaDir)
	scanAcceptanceLibrary(t, client, baseURL, libraryID)
	media := waitAcceptanceMedia(t, client, baseURL, libraryID)
	if len(media) != 2 {
		t.Fatalf("单二进制应扫描到图片和视频，实际 %d", len(media))
	}
	for _, item := range media {
		waitAcceptanceThumbnail(t, client, baseURL, item.ID, 320)
	}
	assertAcceptanceAssets(t, dbPath, media, 2)

	cleanTaskID := enqueueAcceptanceClean(t, client, baseURL)
	waitAcceptanceTask(t, client, baseURL, cleanTaskID)
	for _, item := range media {
		waitAcceptanceThumbnail(t, client, baseURL, item.ID, 320)
	}
	assertAcceptanceAssets(t, dbPath, media, 2)
}

type acceptanceMedia struct {
	ID       int64  `json:"id"`
	FileName string `json:"file_name"`
}

func newThumbnailAcceptanceCommand(t *testing.T, binaryPath, dbPath, tempDir string, port int, ffmpegPath, ffprobePath string) (*exec.Cmd, *os.File, string) {
	t.Helper()
	logPath := filepath.Join(tempDir, "thumbnail-startup.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("创建缩略图验收日志失败: %v", err)
	}
	command := exec.Command(binaryPath)
	command.Env = append(os.Environ(),
		"DB_PATH="+dbPath,
		"SERVER_PORT="+strconv.Itoa(port),
		"JWT_SECRET=fr2-028-acceptance-secret",
		"JIANVIDEO_FFMPEG_PATH="+ffmpegPath,
		"JIANVIDEO_FFPROBE_PATH="+ffprobePath,
		"JIANVIDEO_MAGICK_PATH="+filepath.Join(tempDir, "missing-magick"),
	)
	command.Stdout, command.Stderr = logFile, logFile
	return command, logFile, logPath
}

func newAcceptanceHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建验收 Cookie 容器失败: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func setupAndLogin(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	acceptanceJSON(t, client, http.MethodPost, baseURL+"/api/auth/setup", map[string]string{
		"username": "admin", "password": "admin",
	}, http.StatusOK, http.StatusCreated)
	acceptanceJSON(t, client, http.MethodPost, baseURL+"/api/auth/login", map[string]string{
		"username": "admin", "password": "admin",
	}, http.StatusOK)
}

func createAcceptanceLibrary(t *testing.T, client *http.Client, baseURL, mediaDir string) int64 {
	t.Helper()
	body := acceptanceJSON(t, client, http.MethodPost, baseURL+"/api/library/paths", map[string]string{
		"path": filepath.ToSlash(mediaDir), "type": "local", "label": "FR2-028 单二进制验收",
	}, http.StatusOK, http.StatusCreated)
	var response struct {
		ID int64 `json:"id"`
	}
	decodeAcceptanceJSON(t, body, &response)
	if response.ID <= 0 {
		t.Fatalf("创建媒体库未返回有效 ID: %s", body)
	}
	return response.ID
}

func scanAcceptanceLibrary(t *testing.T, client *http.Client, baseURL string, libraryID int64) {
	t.Helper()
	body := acceptanceJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/library/scan/%d?mode=full", baseURL, libraryID), nil, http.StatusOK, http.StatusAccepted)
	var response struct {
		TaskID int64 `json:"task_id"`
	}
	decodeAcceptanceJSON(t, body, &response)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		tasksBody := acceptanceJSON(t, client, http.MethodGet, baseURL+"/api/library/scan/tasks", nil, http.StatusOK)
		var tasks struct {
			Tasks []struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
				Error  string `json:"error"`
			} `json:"tasks"`
		}
		decodeAcceptanceJSON(t, tasksBody, &tasks)
		for _, task := range tasks.Tasks {
			if task.ID != response.TaskID {
				continue
			}
			if task.Status == "completed" {
				return
			}
			if task.Status == "error" {
				t.Fatalf("单二进制扫描失败: %s", task.Error)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("单二进制扫描任务未在期限内完成")
}

func waitAcceptanceMedia(t *testing.T, client *http.Client, baseURL string, libraryID int64) []acceptanceMedia {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body := acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/library/media?library_id=%d&page_size=100", baseURL, libraryID), nil, http.StatusOK)
		var response struct {
			Items []acceptanceMedia `json:"items"`
		}
		decodeAcceptanceJSON(t, body, &response)
		if len(response.Items) == 2 {
			return response.Items
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("单二进制未返回扫描后的真实素材")
	return nil
}

func waitAcceptanceThumbnail(t *testing.T, client *http.Client, baseURL string, mediaID int64, size int) {
	t.Helper()
	url := fmt.Sprintf("%s/api/library/thumbnail/%d?size=%d&probe=1", baseURL, mediaID, size)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err != nil {
			t.Fatalf("请求单二进制缩略图失败: %v", err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			t.Fatalf("读取单二进制缩略图失败: %v", readErr)
		}
		if response.StatusCode == http.StatusOK {
			if len(body) <= 100 || response.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("单二进制返回的缩略图无效: status=%d type=%s bytes=%d", response.StatusCode, response.Header.Get("Content-Type"), len(body))
			}
			return
		}
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("单二进制缩略图应返回 202 或 200，实际 %d body=%s", response.StatusCode, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("媒体 %d 的缩略图未在期限内生成", mediaID)
}

func enqueueAcceptanceClean(t *testing.T, client *http.Client, baseURL string) int64 {
	t.Helper()
	body := acceptanceJSON(t, client, http.MethodPost, baseURL+"/api/storage/cache/clean", map[string]any{
		"dry_run": false, "kinds": []string{"thumbnail"},
	}, http.StatusAccepted)
	var response struct {
		TaskID int64 `json:"task_id"`
	}
	decodeAcceptanceJSON(t, body, &response)
	return response.TaskID
}

func waitAcceptanceTask(t *testing.T, client *http.Client, baseURL string, taskID int64) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		body := acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d", baseURL, taskID), nil, http.StatusOK)
		var task struct {
			Status string  `json:"status"`
			Error  *string `json:"error"`
		}
		decodeAcceptanceJSON(t, body, &task)
		if task.Status == "succeeded" {
			return
		}
		if task.Status == "failed" {
			t.Fatalf("缓存清理任务失败: %v", task.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("缓存清理任务未在期限内完成")
}

func assertAcceptanceAssets(t *testing.T, dbPath string, media []acceptanceMedia, want int) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开单二进制验收数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM cache_assets WHERE kind = 'thumbnail' AND missing_at IS NULL").Scan(&count); err != nil {
		t.Fatalf("统计单二进制缩略图缓存失败: %v", err)
	}
	if count != want {
		t.Fatalf("单二进制缩略图缓存登记数不符: got=%d want=%d", count, want)
	}
	for _, item := range media {
		var relativePath, variant string
		if err := db.QueryRow("SELECT relative_path, variant FROM cache_assets WHERE kind = 'thumbnail' AND media_id = ?", item.ID).Scan(&relativePath, &variant); err != nil {
			t.Fatalf("查询媒体 %d 缩略图缓存失败: %v", item.ID, err)
		}
		wantPath := filepath.ToSlash(filepath.Join("thumbnails", "space-default", strconv.FormatInt(item.ID, 10), "320.jpg"))
		if relativePath != wantPath || variant != "320" {
			t.Fatalf("单二进制缩略图路径或档位错误: path=%s variant=%s", relativePath, variant)
		}
	}
}

func acceptanceJSON(t *testing.T, client *http.Client, method, url string, payload any, accepted ...int) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("编码验收请求失败: %v", err)
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("创建验收请求失败: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("执行验收请求失败: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取验收响应失败: %v", err)
	}
	for _, status := range accepted {
		if response.StatusCode == status {
			return data
		}
	}
	t.Fatalf("验收请求状态错误: %s %s status=%d body=%s", method, url, response.StatusCode, data)
	return nil
}

func decodeAcceptanceJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("解析验收响应失败: %v body=%s", err, data)
	}
}

func writeRealImage(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建真实图片失败: %v", err)
	}
	defer func() { _ = file.Close() }()
	canvas := image.NewRGBA(image.Rect(0, 0, 800, 450))
	for y := 0; y < 450; y++ {
		for x := 0; x < 800; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	if err := jpeg.Encode(file, canvas, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("编码真实图片失败: %v", err)
	}
}

func writeRealVideo(t *testing.T, ffmpegPath, path string) {
	t.Helper()
	command := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=duration=3:size=640x360:rate=15",
		"-c:v", "mpeg4", "-q:v", "4", path,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("生成真实视频失败: %v\n%s", err, output)
	}
}
