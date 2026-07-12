package main

import (
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
)

func TestSingleBinarySmartCoverUsesRealFFmpegAndRestoresManualSelection(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("需要 ffmpeg 验收真实智能封面")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("需要 ffprobe 验收真实智能封面")
	}
	tempDir := t.TempDir()
	mediaDir := filepath.Join(tempDir, "media")
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		t.Fatalf("创建智能封面验收素材目录失败: %v", err)
	}
	videoPath := filepath.Join(mediaDir, "fr2-059-cover.mp4")
	writeCoverAcceptanceVideo(t, ffmpegPath, videoPath)

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
	mediaID := waitCoverAcceptanceMedia(t, client, baseURL, libraryID)

	generateTaskID := enqueueCoverAcceptanceGeneration(t, client, baseURL, mediaID, false)
	waitAcceptanceTask(t, client, baseURL, generateTaskID)
	generated := loadCoverAcceptanceState(t, client, baseURL, mediaID)
	if len(generated.Candidates) != 5 || generated.Cover == nil {
		t.Fatalf("真实封面候选生成不完整: %+v", generated)
	}
	chosen := generated.Candidates[len(generated.Candidates)-1]
	if chosen.Fingerprint == generated.Cover.SelectedFingerprint {
		t.Fatal("专项验收必须选择非默认候选")
	}
	selected := selectCoverAcceptanceCandidate(t, client, baseURL, mediaID, chosen.ID)
	if !selected.Manual || selected.SelectedFingerprint != chosen.Fingerprint || selected.SelectedAssetID != chosen.AssetID {
		t.Fatalf("人工封面选择语义不完整: %+v", selected)
	}
	oldAssetID := selected.SelectedAssetID
	assertCoverAcceptanceJPEG(t, ffprobePath, dbPath, tempDir, oldAssetID)
	assertCoverAcceptanceAudit(t, dbPath, mediaID)

	cleanTaskID := enqueueCoverAcceptanceClean(t, client, baseURL)
	waitAcceptanceTask(t, client, baseURL, cleanTaskID)
	stale := loadCoverAcceptanceState(t, client, baseURL, mediaID)
	if stale.Cover == nil || !stale.Cover.Manual || stale.Cover.SelectedFingerprint != chosen.Fingerprint || stale.Cover.SelectedAssetID != oldAssetID {
		t.Fatalf("缓存清理后人工选择语义丢失: %+v", stale.Cover)
	}
	assertCoverAcceptanceAssetCount(t, dbPath, mediaID, 0)

	rebuildTaskID := enqueueCoverAcceptanceGeneration(t, client, baseURL, mediaID, false)
	waitAcceptanceTask(t, client, baseURL, rebuildTaskID)
	restored := loadCoverAcceptanceState(t, client, baseURL, mediaID)
	if restored.Cover == nil || !restored.Cover.Manual || restored.Cover.SelectedFingerprint != chosen.Fingerprint {
		t.Fatalf("重建后未恢复人工选择指纹: %+v", restored.Cover)
	}
	if restored.Cover.SelectedAssetID <= 0 || restored.Cover.SelectedAssetID == oldAssetID {
		t.Fatalf("重建后 selected_asset_id 未更新: old=%d cover=%+v", oldAssetID, restored.Cover)
	}
	assertCoverAcceptanceAssetCount(t, dbPath, mediaID, 5)
	assertCoverAcceptanceJPEG(t, ffprobePath, dbPath, tempDir, restored.Cover.SelectedAssetID)

	invalidateCoverAcceptanceSource(t, dbPath, mediaID, filepath.Join(tempDir, "missing-source.mp4"))
	refreshTaskID := enqueueCoverAcceptanceGeneration(t, client, baseURL, mediaID, true)
	waitCoverAcceptanceAttempt(t, client, baseURL, refreshTaskID)
	afterInvalidSource := loadCoverAcceptanceState(t, client, baseURL, mediaID)
	if afterInvalidSource.Cover == nil || !afterInvalidSource.Cover.Manual || afterInvalidSource.Cover.SelectedFingerprint != chosen.Fingerprint || afterInvalidSource.Cover.SelectedAssetID != restored.Cover.SelectedAssetID {
		t.Fatalf("源视频失效时不得静默换帧: before=%+v after=%+v", restored.Cover, afterInvalidSource.Cover)
	}
}

type coverAcceptanceCandidate struct {
	ID               int64   `json:"id"`
	AssetID          int64   `json:"asset_id"`
	TimestampSeconds float64 `json:"timestamp_seconds"`
	Fingerprint      string  `json:"fingerprint"`
}

type coverAcceptanceSelection struct {
	SelectedAssetID     int64  `json:"selected_asset_id"`
	SelectedFingerprint string `json:"selected_fingerprint"`
	Manual              bool   `json:"manual"`
}

type coverAcceptanceState struct {
	Cover      *coverAcceptanceSelection  `json:"cover"`
	Candidates []coverAcceptanceCandidate `json:"candidates"`
}

func writeCoverAcceptanceVideo(t *testing.T, ffmpegPath, path string) {
	t.Helper()
	command := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc2=duration=6:size=640x360:rate=15",
		"-c:v", "mpeg4", "-q:v", "4", "-movflags", "+faststart", path,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("生成智能封面真实视频失败: %v\n%s", err, output)
	}
}

func waitCoverAcceptanceMedia(t *testing.T, client *http.Client, baseURL string, libraryID int64) int64 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		body := acceptanceJSON(t, client, "GET", fmt.Sprintf("%s/api/library/media?library_id=%d&page_size=100", baseURL, libraryID), nil, 200)
		var response struct {
			Items []acceptanceMedia `json:"items"`
		}
		decodeAcceptanceJSON(t, body, &response)
		if len(response.Items) == 1 {
			return response.Items[0].ID
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("单二进制未返回智能封面验收视频")
	return 0
}

func enqueueCoverAcceptanceGeneration(t *testing.T, client *http.Client, baseURL string, mediaID int64, refresh bool) int64 {
	t.Helper()
	body := acceptanceJSON(t, client, "POST", fmt.Sprintf("%s/api/library/media/%d/covers/generate", baseURL, mediaID), map[string]bool{"refresh": refresh}, 202)
	var response struct {
		TaskID int64 `json:"task_id"`
	}
	decodeAcceptanceJSON(t, body, &response)
	if response.TaskID <= 0 {
		t.Fatalf("封面生成未返回有效任务 ID: %s", body)
	}
	return response.TaskID
}

func loadCoverAcceptanceState(t *testing.T, client *http.Client, baseURL string, mediaID int64) coverAcceptanceState {
	t.Helper()
	body := acceptanceJSON(t, client, "GET", fmt.Sprintf("%s/api/library/media/%d/covers", baseURL, mediaID), nil, 200)
	var state coverAcceptanceState
	decodeAcceptanceJSON(t, body, &state)
	return state
}

func selectCoverAcceptanceCandidate(t *testing.T, client *http.Client, baseURL string, mediaID, candidateID int64) coverAcceptanceSelection {
	t.Helper()
	body := acceptanceJSON(t, client, "PUT", fmt.Sprintf("%s/api/library/media/%d/cover", baseURL, mediaID), map[string]int64{"candidate_id": candidateID}, 200)
	var selected coverAcceptanceSelection
	decodeAcceptanceJSON(t, body, &selected)
	return selected
}

func enqueueCoverAcceptanceClean(t *testing.T, client *http.Client, baseURL string) int64 {
	t.Helper()
	body := acceptanceJSON(t, client, "POST", baseURL+"/api/storage/cache/clean", map[string]any{"dry_run": false, "kinds": []string{"cover"}}, 202)
	var response struct {
		TaskID int64 `json:"task_id"`
	}
	decodeAcceptanceJSON(t, body, &response)
	return response.TaskID
}

func assertCoverAcceptanceAudit(t *testing.T, dbPath string, mediaID int64) {
	t.Helper()
	db := openCoverAcceptanceDB(t, dbPath)
	defer func() { _ = db.Close() }()
	for _, action := range []string{"cover.generated", "cover.selected"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action = ? AND resource_type = 'media' AND resource_id = ?", action, strconv.FormatInt(mediaID, 10)).Scan(&count); err != nil {
			t.Fatalf("查询封面审计失败: %v", err)
		}
		if count < 1 {
			t.Fatalf("缺少封面审计事件 %s", action)
		}
	}
}

func assertCoverAcceptanceAssetCount(t *testing.T, dbPath string, mediaID int64, want int) {
	t.Helper()
	db := openCoverAcceptanceDB(t, dbPath)
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM cache_assets WHERE kind = 'cover' AND media_id = ?", mediaID).Scan(&count); err != nil {
		t.Fatalf("统计封面缓存资产失败: %v", err)
	}
	if count != want {
		t.Fatalf("封面缓存资产数不符: got=%d want=%d", count, want)
	}
}

func assertCoverAcceptanceJPEG(t *testing.T, ffprobePath, dbPath, dataDir string, assetID int64) {
	t.Helper()
	db := openCoverAcceptanceDB(t, dbPath)
	defer func() { _ = db.Close() }()
	var relativePath string
	if err := db.QueryRow("SELECT relative_path FROM cache_assets WHERE id = ? AND kind = 'cover'", assetID).Scan(&relativePath); err != nil {
		t.Fatalf("读取封面缓存路径失败: %v", err)
	}
	path := filepath.Join(dataDir, filepath.FromSlash(relativePath))
	command := exec.Command(ffprobePath, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height", "-of", "json", path)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("ffprobe 验证封面 JPEG 失败: %v", err)
	}
	var probe struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		t.Fatalf("解析封面 ffprobe 结果失败: %v", err)
	}
	if len(probe.Streams) != 1 || probe.Streams[0].CodecName != "mjpeg" || probe.Streams[0].Width != 640 || probe.Streams[0].Height <= 0 {
		t.Fatalf("真实封面文件格式或尺寸不符: %+v", probe.Streams)
	}
}

func invalidateCoverAcceptanceSource(t *testing.T, dbPath string, mediaID int64, missingPath string) {
	t.Helper()
	db := openCoverAcceptanceDB(t, dbPath)
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE media_files SET file_path = ?, file_state = 'available' WHERE id = ?", missingPath, mediaID); err != nil {
		t.Fatalf("模拟源视频失效失败: %v", err)
	}
}

func waitCoverAcceptanceAttempt(t *testing.T, client *http.Client, baseURL string, taskID int64) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		body := acceptanceJSON(t, client, "GET", fmt.Sprintf("%s/api/tasks/%d", baseURL, taskID), nil, 200)
		var task struct {
			Status   string `json:"status"`
			Attempts int    `json:"attempts"`
		}
		decodeAcceptanceJSON(t, body, &task)
		if task.Status == "succeeded" {
			t.Fatal("源视频失效后的封面刷新不得成功")
		}
		if task.Attempts >= 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("源视频失效后的封面刷新任务未执行")
}

func openCoverAcceptanceDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开智能封面验收数据库失败: %v", err)
	}
	return db
}
