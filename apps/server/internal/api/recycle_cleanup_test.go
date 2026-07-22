package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// setupCleanupRouter 构建带 library + settings 服务的测试路由，并返回服务以便造数据。
func setupCleanupRouter(t *testing.T) (*gin.Engine, *library.Service, *settings.Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	libSvc := library.NewService(gdb)
	setSvc := settings.NewService(gdb)
	h := NewHandler(libSvc).WithSettings(setSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, libSvc, setSvc, gdb
}

// softDeleteOne 造一条软删媒体（带真实源文件），返回源文件磁盘路径。
func softDeleteOne(t *testing.T, libSvc *library.Service, gdb *gorm.DB, dir, name string) string {
	t.Helper()
	src := filepath.Join(dir, name)
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	mf, err := libSvc.CreateMediaFile(1, filepath.ToSlash(src), 100)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", mf.ID).
		Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	return src
}

// TestRecycleCleanup_UnsetPathRejected 未配置盘符路径时返回 409 且不移动文件。
func TestRecycleCleanup_UnsetPathRejected(t *testing.T) {
	r, libSvc, setSvc, gdb := setupCleanupRouter(t)
	dir := t.TempDir()
	src := softDeleteOne(t, libSvc, gdb, dir, "a.mp4")
	// 故意配置一个不含该盘符的映射
	_ = setSvc.Set(settings.KeyRecycleBinPaths, `{"Z":"Z:/.recycle"}`)

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("未配路径期望 409, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("拒绝后源文件应仍在, 实际: %v", err)
	}
	var deleted []models.MediaFile
	gdb.Where("deleted_at IS NOT NULL").Find(&deleted)
	if len(deleted) != 1 {
		t.Fatalf("拒绝后软删记录应仍在, 实际 %d 条", len(deleted))
	}
}

// TestRecycleCleanup_Success 配置好盘符后清理：文件移动、记录删除、返回统计。
func TestRecycleCleanup_Success(t *testing.T) {
	r, libSvc, setSvc, gdb := setupCleanupRouter(t)
	dir := t.TempDir()
	src := softDeleteOne(t, libSvc, gdb, dir, "b.mp4")

	drive := ""
	if len(src) >= 2 && src[1] == ':' {
		drive = string(src[0])
	}
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过成功用例")
	}
	recycleDir := filepath.Join(dir, ".recycle")
	_ = setSvc.Set(settings.KeyRecycleBinPaths, `{"`+drive+`":"`+filepath.ToSlash(recycleDir)+`"}`)

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("清理期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Moved  int `json:"moved"`
		Failed int `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp.Moved != 1 || resp.Failed != 0 {
		t.Fatalf("期望 moved=1 failed=0, 实际 %+v", resp)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("清理后源文件应已移走, 实际 stat err=%v", err)
	}
	var deleted []models.MediaFile
	gdb.Where("deleted_at IS NOT NULL").Find(&deleted)
	if len(deleted) != 0 {
		t.Fatalf("清理后回收站应为空, 实际 %d 条", len(deleted))
	}
}

// TestRecycleCleanup_RecoveryConflictReturnsSafePartialResult 恢复冲突仅返回稳定信息与文件名。
func TestRecycleCleanup_RecoveryConflictReturnsSafePartialResult(t *testing.T) {
	r, libSvc, setSvc, gdb := setupCleanupRouter(t)
	dir := t.TempDir()
	src := softDeleteOne(t, libSvc, gdb, dir, "恢复冲突.mp4")
	drive := ""
	if len(src) >= 2 && src[1] == ':' {
		drive = string(src[0])
	}
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过恢复冲突 API 用例")
	}
	recycleDir := filepath.Join(dir, ".recycle")
	if err := setSvc.Set(settings.KeyRecycleBinPaths, `{"`+drive+`":"`+filepath.ToSlash(recycleDir)+`"}`); err != nil {
		t.Fatalf("写入回收站路径设置失败: %v", err)
	}
	var media models.MediaFile
	if err := gdb.Where("file_path = ?", filepath.ToSlash(src)).First(&media).Error; err != nil {
		t.Fatalf("读取软删媒体失败: %v", err)
	}

	const callbackName = "test:api_recycle_recovery_conflict"
	injectedErr := errors.New("注入数据库删除失败")
	failed := false
	if err := gdb.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if failed || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" {
			return
		}
		failed = true
		if err := os.WriteFile(src, []byte("用户新文件"), 0o644); err != nil {
			if addErr := tx.AddError(err); !errors.Is(addErr, err) {
				t.Errorf("GORM 未保留源文件写入错误注入: got=%v want=%v", addErr, err)
			}
			return
		}
		if addErr := tx.AddError(injectedErr); !errors.Is(addErr, injectedErr) {
			t.Errorf("GORM 未保留数据库删除错误注入: got=%v want=%v", addErr, injectedErr)
		}
	}); err != nil {
		t.Fatalf("注册恢复冲突失败注入失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.Callback().Delete().Remove(callbackName) })

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("恢复冲突期望 409, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		Moved      int    `json:"moved"`
		Failed     int    `json:"failed"`
		MediaID    int64  `json:"media_id"`
		SourceName string `json:"source_name"`
		TargetName string `json:"target_name"`
		State      string `json:"state"`
		Action     string `json:"action"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("恢复冲突响应解析失败: %v", err)
	}
	if resp.Code != "RECYCLE_RECOVERY_CONFLICT" || resp.Moved != 0 || resp.Failed != 1 || resp.MediaID != media.ID {
		t.Fatalf("恢复冲突响应未保留部分结果与媒体标识: %+v", resp)
	}
	if resp.SourceName != filepath.Base(src) || resp.TargetName != ".jianvideo-"+strconv.FormatInt(media.ID, 10)+"-恢复冲突.mp4" {
		t.Fatalf("恢复冲突响应只能返回文件名: %+v", resp)
	}
	if resp.State != library.RecycleRecoveryStateSourceOccupied || !strings.Contains(resp.Action, "保留") {
		t.Fatalf("恢复冲突响应缺少安全恢复状态与动作: %+v", resp)
	}
	body := w.Body.String()
	for _, secret := range []string{"注入数据库删除失败", filepath.ToSlash(dir), filepath.ToSlash(src), filepath.ToSlash(recycleDir)} {
		if strings.Contains(body, secret) {
			t.Fatalf("恢复冲突响应泄露内部信息 %q: %s", secret, body)
		}
	}
	if strings.Contains(body, `"source"`) || strings.Contains(body, `"target"`) {
		t.Fatalf("恢复冲突响应不得返回绝对路径字段: %s", body)
	}
	if got, err := os.ReadFile(src); err != nil || string(got) != "用户新文件" {
		t.Fatalf("API 恢复冲突不得移动或删除新源文件: content=%q err=%v", got, err)
	}
}

func TestWriteRecycleRecoveryError_RestoreFailedRedactsInternalDetails(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	recovery := &library.RecycleRecoveryError{
		MediaID:       42,
		Source:        filepath.ToSlash(filepath.Join(t.TempDir(), "源文件.mp4")),
		Target:        filepath.ToSlash(filepath.Join(t.TempDir(), "回收目标.mp4")),
		State:         library.RecycleRecoveryStateRestoreFailed,
		Action:        "保留数据库记录与回收站目标，检查文件系统后重试",
		DatabaseError: errors.New("注入数据库删除失败: internal-db-marker"),
		RecoveryError: errors.New("注入恢复失败: C:/private/recycle/target.mp4"),
	}
	var logs bytes.Buffer
	originalLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLogOutput) })

	writeRecycleRecoveryError(c, library.CleanupResult{Moved: 2, Failed: 1}, recovery)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("恢复失败期望 500, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		Moved      int    `json:"moved"`
		Failed     int    `json:"failed"`
		MediaID    int64  `json:"media_id"`
		SourceName string `json:"source_name"`
		TargetName string `json:"target_name"`
		State      string `json:"state"`
		Action     string `json:"action"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("恢复失败响应解析失败: %v", err)
	}
	if resp.Code != "RECYCLE_RECOVERY_FAILED" || resp.Message != "回收站恢复失败：文件与数据库状态未能自动同步，请检查服务端日志" {
		t.Fatalf("恢复失败应返回稳定 message/code: %+v", resp)
	}
	if resp.Moved != 2 || resp.Failed != 1 || resp.MediaID != 42 || resp.State != library.RecycleRecoveryStateRestoreFailed {
		t.Fatalf("恢复失败响应缺少安全恢复上下文: %+v", resp)
	}
	if resp.SourceName != "源文件.mp4" || resp.TargetName != "回收目标.mp4" || !strings.Contains(resp.Action, "保留") {
		t.Fatalf("恢复失败响应只能返回文件名与安全动作: %+v", resp)
	}
	body := w.Body.String()
	for _, secret := range []string{recovery.Source, recovery.Target, recovery.DatabaseError.Error(), recovery.RecoveryError.Error(), "internal-db-marker", "C:/private"} {
		if strings.Contains(body, secret) {
			t.Fatalf("恢复失败响应泄露内部信息 %q: %s", secret, body)
		}
	}
	if strings.Contains(recovery.Error(), recovery.DatabaseError.Error()) || strings.Contains(recovery.Error(), recovery.RecoveryError.Error()) {
		t.Fatalf("恢复错误概述不得拼接底层错误: %s", recovery.Error())
	}
	logText := logs.String()
	for _, detail := range []string{"[ERROR] 回收站恢复异常", recovery.Source, recovery.Target, recovery.DatabaseError.Error(), recovery.RecoveryError.Error()} {
		if !strings.Contains(logText, detail) {
			t.Fatalf("服务端日志缺少完整恢复诊断 %q: %s", detail, logText)
		}
	}
}

func TestWriteRecycleRecoveryError_SQLiteBusyOrLockedTakesPriority(t *testing.T) {
	cases := []struct {
		name        string
		databaseErr error
		recoveryErr error
	}{
		{
			name:        "数据库 busy 被包装",
			databaseErr: fmt.Errorf("内部数据库路径: %w", sqlite3.Error{Code: sqlite3.ErrBusy}),
		},
		{
			name:        "恢复 locked 被包装",
			recoveryErr: fmt.Errorf("内部恢复路径: %w", sqlite3.Error{Code: sqlite3.ErrLocked}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			recovery := &library.RecycleRecoveryError{
				MediaID:       42,
				Source:        "C:/private/source.mp4",
				Target:        "C:/private/recycle/target.mp4",
				State:         library.RecycleRecoveryStateSourceOccupied,
				Action:        "保留数据库记录与可恢复文件",
				DatabaseError: tc.databaseErr,
				RecoveryError: tc.recoveryErr,
			}

			writeRecycleRecoveryError(c, library.CleanupResult{Failed: 1}, recovery)

			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("recovery 包裹 SQLite busy/locked 必须优先返回 503, 实际 %d, body: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析 recovery busy/locked 响应失败: %v", err)
			}
			if resp.Code != "CLEANUP_UNAVAILABLE" || resp.Message != "回收站清理暂时不可用" {
				t.Fatalf("recovery busy/locked 响应必须稳定: %+v", resp)
			}
			for _, secret := range []string{"C:/private", "database is", "内部数据库路径", "内部恢复路径"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatalf("recovery busy/locked 响应泄露内部信息 %q: %s", secret, w.Body.String())
				}
			}
		})
	}
}

func TestRestoreMediaFile_DatabaseUnavailableReturnsStable503(t *testing.T) {
	r, libSvc, _, gdb := setupCleanupRouter(t)
	mf, err := libSvc.CreateMediaFile(1, "/tmp/restore-database-unavailable.mp4", 1024)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	if err := libSvc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库连接失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭数据库失败: %v", err)
	}

	var logs bytes.Buffer
	originalLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLogOutput) })

	req := httptest.NewRequest("POST", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/restore", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("数据库不可用时还原应返回 503, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析数据库不可用响应失败: %v", err)
	}
	if resp.Code != "RESTORE_UNAVAILABLE" || resp.Message != "回收站还原暂时不可用" {
		t.Fatalf("数据库不可用响应必须稳定且不泄露内部信息: %+v", resp)
	}
	body := w.Body.String()
	for _, secret := range []string{"database is closed", "restore-database-unavailable.mp4", "/tmp/"} {
		if strings.Contains(body, secret) {
			t.Fatalf("数据库不可用响应泄露内部信息 %q: %s", secret, body)
		}
	}
	logText := logs.String()
	for _, detail := range []string{"[ERROR] 回收站还原失败", "mediaID=" + strconv.FormatInt(mf.ID, 10), "database is closed"} {
		if !strings.Contains(logText, detail) {
			t.Fatalf("服务端中文结构化日志缺少诊断 %q: %s", detail, logText)
		}
	}
}

func TestWriteRecycleCleanupError_OrdinaryErrorLogsChineseContext(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	result := library.CleanupResult{Moved: 2, Failed: 1}
	cleanupErr := errors.New("磁盘处理失败: C:/private/recycle/target.mp4")
	var logs bytes.Buffer
	originalLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLogOutput) })

	writeRecycleCleanupError(c, "space-test", result, cleanupErr)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("普通清理错误期望 500, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析普通清理错误响应失败: %v", err)
	}
	if resp.Code != "CLEANUP_FAILED" || resp.Message != "清理回收站失败" {
		t.Fatalf("普通清理错误响应必须稳定: %+v", resp)
	}
	if strings.Contains(w.Body.String(), cleanupErr.Error()) || strings.Contains(w.Body.String(), "C:/private") {
		t.Fatalf("普通清理错误响应不得泄露底层错误或路径: %s", w.Body.String())
	}
	for _, detail := range []string{"[ERROR] 回收站清理失败", "spaceID=space-test", "moved=2", "failed=1", cleanupErr.Error()} {
		if !strings.Contains(logs.String(), detail) {
			t.Fatalf("普通清理错误日志缺少上下文 %q: %s", detail, logs.String())
		}
	}
}

func TestWriteRecycleCleanupError_SQLiteBusyOrLockedReturnsStable503(t *testing.T) {
	cases := []struct {
		name string
		code sqlite3.ErrNo
	}{
		{name: "busy", code: sqlite3.ErrBusy},
		{name: "locked", code: sqlite3.ErrLocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			cleanupErr := fmt.Errorf("C:/private/recycle.db: %w", sqlite3.Error{Code: tc.code})
			var logs bytes.Buffer
			originalLogOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(originalLogOutput) })

			writeRecycleCleanupError(c, "space-busy", library.CleanupResult{Moved: 1, Failed: 1}, cleanupErr)

			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("SQLite %s 期望 503, 实际 %d, body: %s", tc.name, w.Code, w.Body.String())
			}
			var resp struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析 SQLite %s 响应失败: %v", tc.name, err)
			}
			if resp.Code != "CLEANUP_UNAVAILABLE" || resp.Message != "回收站清理暂时不可用" {
				t.Fatalf("SQLite %s 响应必须稳定: %+v", tc.name, resp)
			}
			for _, secret := range []string{"database is", "C:/private", cleanupErr.Error()} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatalf("SQLite %s 响应泄露底层信息 %q: %s", tc.name, secret, w.Body.String())
				}
			}
			for _, detail := range []string{"[ERROR] 回收站清理失败", "spaceID=space-busy", "moved=1", "failed=1"} {
				if !strings.Contains(logs.String(), detail) {
					t.Fatalf("SQLite %s 日志缺少上下文 %q: %s", tc.name, detail, logs.String())
				}
			}
		})
	}
}

// TestRecycleCleanup_SettingsUnavailable 未注入 settings 服务返回 503。
func TestRecycleCleanup_SettingsUnavailable(t *testing.T) {
	h := NewHandler(library.NewService(nil))
	r := gin.New()
	RegisterRoutes(r, h)

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用设置服务期望 503, 实际 %d", w.Code)
	}
}
