package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSingleBinaryGeneratesAndRebuildsRealABRHLS(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("需要 ffmpeg 验收真实 ABR 转码")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("需要 ffprobe 验收真实 ABR 产物")
	}

	tempDir := t.TempDir()
	mediaDir := filepath.Join(tempDir, "media")
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		t.Fatalf("创建 ABR 验收素材目录失败: %v", err)
	}
	writeABRAcceptanceVideo(t, ffmpegPath, filepath.Join(mediaDir, "multi-720p.mp4"), "1280x720")
	writeABRAcceptanceVideo(t, ffmpegPath, filepath.Join(mediaDir, "source-360p.mp4"), "640x360")

	dbPath := filepath.Join(tempDir, "jianvideo-abr.sqlite")
	binaryPath := buildJianVideoBinary(t, tempDir)
	port := reserveTCPPort(t)
	command, logFile, logPath := newABRAcceptanceCommand(t, binaryPath, dbPath, tempDir, port, ffmpegPath, ffprobePath)
	waitDone := startAcceptanceCommand(t, command, logFile)
	waitForBinaryStartup(t, port, waitDone, logPath)

	client := newAcceptanceHTTPClient(t)
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	setupAndLogin(t, client, baseURL)
	configureABRAcceptance(t, client, baseURL)
	libraryID := createAcceptanceLibrary(t, client, baseURL, mediaDir)
	scanAcceptanceLibrary(t, client, baseURL, libraryID)
	media := waitAcceptanceMedia(t, client, baseURL, libraryID)
	mediaByName := acceptanceMediaIDs(t, media)

	assertNoAutomaticABRTasks(t, dbPath)
	assertDirectStreamAvailable(t, client, baseURL, mediaByName["multi-720p.mp4"])
	assertDirectStreamAvailable(t, client, baseURL, mediaByName["source-360p.mp4"])

	expectations := map[int64]map[string]string{
		mediaByName["multi-720p.mp4"]:  {"720p": "1280x720", "480p": "854x480"},
		mediaByName["source-360p.mp4"]: {"source": "640x360"},
	}
	generateAndAssertABR(t, client, baseURL, tempDir, ffprobePath, expectations, true)
	assertDirectStreamAvailable(t, client, baseURL, mediaByName["multi-720p.mp4"])

	cleanABRAcceptanceCache(t, client, baseURL)
	assertABRCacheEmpty(t, client, baseURL, expectations)
	generateAndAssertABR(t, client, baseURL, tempDir, ffprobePath, expectations, false)
}

func newABRAcceptanceCommand(t *testing.T, binaryPath, dbPath, tempDir string, port int, ffmpegPath, ffprobePath string) (*exec.Cmd, *os.File, string) {
	t.Helper()
	logPath := filepath.Join(tempDir, "abr-startup.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("创建 ABR 验收日志失败: %v", err)
	}
	command := exec.Command(binaryPath)
	command.Env = append(os.Environ(),
		"DB_PATH="+dbPath,
		"SERVER_PORT="+strconv.Itoa(port),
		"JWT_SECRET=fr2-026-acceptance-secret",
		"JIANVIDEO_FFMPEG_PATH="+ffmpegPath,
		"JIANVIDEO_FFPROBE_PATH="+ffprobePath,
		"JIANVIDEO_MAGICK_PATH="+filepath.Join(tempDir, "missing-magick"),
	)
	command.Stdout, command.Stderr = logFile, logFile
	return command, logFile, logPath
}

func writeABRAcceptanceVideo(t *testing.T, ffmpegPath, outputPath, size string) {
	t.Helper()
	command := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=duration=2:size="+size+":rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "mpeg4", "-q:v", "4", "-c:a", "aac", "-shortest", outputPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("生成 ABR 验收视频失败: %v\n%s", err, output)
	}
}

func configureABRAcceptance(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	acceptanceJSON(t, client, http.MethodPut, baseURL+"/api/settings", map[string]any{
		"settings": map[string]string{
			"transcode_hwaccel_mode": "software",
			"transcode_abr_ladder":   `["1080p","720p","480p"]`,
		},
	}, http.StatusOK)
}

func acceptanceMediaIDs(t *testing.T, media []acceptanceMedia) map[string]int64 {
	t.Helper()
	result := make(map[string]int64, len(media))
	for _, item := range media {
		result[item.FileName] = item.ID
	}
	for _, name := range []string{"multi-720p.mp4", "source-360p.mp4"} {
		if result[name] <= 0 {
			t.Fatalf("未扫描到 ABR 验收素材 %s: %+v", name, media)
		}
	}
	return result
}

func assertNoAutomaticABRTasks(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=10000")
	if err != nil {
		t.Fatalf("打开 ABR 验收数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE type = 'transcode.hls.abr'").Scan(&count); err != nil {
		t.Fatalf("统计自动 ABR 任务失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("扫描后不得自动创建高成本 ABR 任务，实际 %d", count)
	}
}

func assertDirectStreamAvailable(t *testing.T, client *http.Client, baseURL string, mediaID int64) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/play/%d/stream", baseURL, mediaID), nil)
	if err != nil {
		t.Fatalf("创建直连验收请求失败: %v", err)
	}
	request.Header.Set("Range", "bytes=0-1023")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("执行直连验收请求失败: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusPartialContent || len(body) == 0 {
		t.Fatalf("原文件直连应优先可用: status=%d bytes=%d body=%s", response.StatusCode, len(body), body)
	}
}

func generateAndAssertABR(t *testing.T, client *http.Client, baseURL, dataDir, ffprobePath string, expectations map[int64]map[string]string, force bool) {
	t.Helper()
	for mediaID, variants := range expectations {
		body := acceptanceJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/play/%d/hls-abr", baseURL, mediaID), map[string]any{
			"priority": 8, "force_rebuild": force,
		}, http.StatusAccepted)
		var response struct {
			TaskID int64 `json:"task_id"`
		}
		decodeAcceptanceJSON(t, body, &response)
		waitAcceptanceTask(t, client, baseURL, response.TaskID)
		assertABRAcceptanceOutput(t, client, baseURL, dataDir, ffprobePath, mediaID, variants)
	}
}

func assertABRAcceptanceOutput(t *testing.T, client *http.Client, baseURL, dataDir, ffprobePath string, mediaID int64, variants map[string]string) {
	t.Helper()
	statusBody := acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/play/%d/hls-status?profile_id=abr-h264", baseURL, mediaID), nil, http.StatusOK)
	var status struct {
		Available bool   `json:"available"`
		URL       string `json:"url"`
	}
	decodeAcceptanceJSON(t, statusBody, &status)
	if !status.Available {
		t.Fatalf("媒体 %d 的 ABR master 未就绪", mediaID)
	}
	master := string(acceptanceJSON(t, client, http.MethodGet, baseURL+status.URL, nil, http.StatusOK))
	if strings.Count(master, "#EXT-X-STREAM-INF") != len(variants) {
		t.Fatalf("媒体 %d 的 master 档位数错误: %s", mediaID, master)
	}
	for variant, dimensions := range variants {
		assertABRVariant(t, client, baseURL, dataDir, ffprobePath, mediaID, variant, dimensions, master)
	}
	assertABRCacheAssets(t, client, baseURL, mediaID, variants)
}

func assertABRVariant(t *testing.T, client *http.Client, baseURL, dataDir, ffprobePath string, mediaID int64, variant, dimensions, master string) {
	t.Helper()
	playlist := variant + "/index.m3u8"
	if !strings.Contains(master, playlist) {
		t.Fatalf("媒体 %d 的 master 缺少 %s: %s", mediaID, playlist, master)
	}
	acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/play/hls/%d/profiles/abr-h264/%s", baseURL, mediaID, playlist), nil, http.StatusOK)
	segments, err := filepath.Glob(filepath.Join(dataDir, "hls", "space-default", strconv.FormatInt(mediaID, 10), "abr-h264", variant, "segment_*.ts"))
	if err != nil || len(segments) == 0 {
		t.Fatalf("媒体 %d 档位 %s 未生成 TS 分片: %v", mediaID, variant, err)
	}
	output, err := exec.Command(ffprobePath, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", segments[0]).CombinedOutput()
	if err != nil || !allProbeDimensionsMatch(string(output), dimensions) {
		t.Fatalf("ffprobe 校验媒体 %d 档位 %s 失败: got=%q want=%s err=%v", mediaID, variant, output, dimensions, err)
	}
}

func allProbeDimensionsMatch(output, dimensions string) bool {
	matched := false
	for _, line := range strings.Fields(output) {
		if line != dimensions {
			return false
		}
		matched = true
	}
	return matched
}

func assertABRCacheAssets(t *testing.T, client *http.Client, baseURL string, mediaID int64, variants map[string]string) {
	t.Helper()
	body := acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/storage/cache/assets?kind=hls&media_id=%d&page_size=20", baseURL, mediaID), nil, http.StatusOK)
	var page struct {
		Items []struct {
			ProfileID string `json:"profile_id"`
			Variant   string `json:"variant"`
		} `json:"items"`
	}
	decodeAcceptanceJSON(t, body, &page)
	seen := map[string]bool{}
	for _, item := range page.Items {
		if item.ProfileID == "abr-h264" {
			seen[item.Variant] = true
		}
	}
	if !seen["master"] {
		t.Fatalf("媒体 %d 未登记 ABR master 缓存", mediaID)
	}
	for variant := range variants {
		if !seen[variant] {
			t.Fatalf("媒体 %d 未登记 ABR 档位缓存 %s", mediaID, variant)
		}
	}
}

func cleanABRAcceptanceCache(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	body := acceptanceJSON(t, client, http.MethodPost, baseURL+"/api/storage/cache/clean", map[string]any{
		"dry_run": false, "kinds": []string{"hls"},
	}, http.StatusAccepted)
	var response struct {
		TaskID int64 `json:"task_id"`
	}
	decodeAcceptanceJSON(t, body, &response)
	waitAcceptanceTask(t, client, baseURL, response.TaskID)
}

func assertABRCacheEmpty(t *testing.T, client *http.Client, baseURL string, expectations map[int64]map[string]string) {
	t.Helper()
	for mediaID := range expectations {
		statusBody := acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/play/%d/hls-status?profile_id=abr-h264", baseURL, mediaID), nil, http.StatusOK)
		var status struct {
			Available bool `json:"available"`
		}
		decodeAcceptanceJSON(t, statusBody, &status)
		if status.Available {
			t.Fatalf("媒体 %d 的 ABR 产物清理后仍可用", mediaID)
		}
		body := acceptanceJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/storage/cache/assets?kind=hls&media_id=%d&page_size=20", baseURL, mediaID), nil, http.StatusOK)
		var page struct {
			Total int64 `json:"total"`
		}
		decodeAcceptanceJSON(t, body, &page)
		if page.Total != 0 {
			t.Fatalf("媒体 %d 的 ABR 缓存登记清理后仍有 %d 条", mediaID, page.Total)
		}
	}
}
