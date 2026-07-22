package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newHealthTestDB 创建带健康巡检相关表的内存测试库（单连接，使内存库表现为共享）。
func newHealthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.MediaFile{}, &models.MediaHealthIssue{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return gdb
}

// healthChecks 构造测试用的判定依赖，默认全部健康（文件存在、视频可解析、缩略图可生成）。
func healthChecks() mediaHealthChecks {
	return mediaHealthChecks{
		stat:       func(string) (os.FileInfo, error) { return nil, nil },
		probeVideo: func(string) error { return nil },
		thumbnail:  func(string) error { return nil },
		isVideo:    func(string) bool { return true },
	}
}

func TestClassifyMediaIssues_ZeroByte(t *testing.T) {
	mf := models.MediaFile{ID: 1, FilePath: "D:/v/a.mp4", FileSize: 0}
	issues := classifyMediaIssues(mf, healthChecks())
	if len(issues) != 1 || issues[0].IssueType != models.HealthIssueZeroByte {
		t.Fatalf("应判定为 0 字节，实际: %+v", issues)
	}
}

func TestClassifyMediaIssues_Missing(t *testing.T) {
	checks := healthChecks()
	checks.stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	mf := models.MediaFile{ID: 1, FilePath: "D:/v/gone.mp4", FileSize: 100}
	issues := classifyMediaIssues(mf, checks)
	if len(issues) != 1 || issues[0].IssueType != models.HealthIssueMissing {
		t.Fatalf("应判定为文件缺失，实际: %+v", issues)
	}
}

func TestClassifyMediaIssues_SkipSMB(t *testing.T) {
	checks := healthChecks()
	// SMB 路径不应触发 os.Stat 缺失判定（排除远程路径）
	checks.stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	mf := models.MediaFile{ID: 1, FilePath: "smb://host/share/a.mp4", FileSize: 100}
	issues := classifyMediaIssues(mf, checks)
	if len(issues) != 0 {
		t.Fatalf("SMB 路径应跳过缺失判定，实际: %+v", issues)
	}
}

func TestClassifyMediaIssues_BrokenVideo(t *testing.T) {
	checks := healthChecks()
	checks.probeVideo = func(string) error { return errors.New("moov atom not found") }
	mf := models.MediaFile{ID: 1, FilePath: "D:/v/broken.mp4", FileSize: 100}
	issues := classifyMediaIssues(mf, checks)
	if len(issues) != 1 || issues[0].IssueType != models.HealthIssueBroken {
		t.Fatalf("应判定为视频损坏，实际: %+v", issues)
	}
}

func TestClassifyMediaIssues_ImageSkipsProbe(t *testing.T) {
	checks := healthChecks()
	checks.isVideo = func(string) bool { return false }
	// 即便 probe 会失败，图片也不应走视频探测
	checks.probeVideo = func(string) error { return errors.New("不该被调用") }
	mf := models.MediaFile{ID: 1, FilePath: "D:/v/p.jpg", FileSize: 100}
	issues := classifyMediaIssues(mf, checks)
	if len(issues) != 0 {
		t.Fatalf("图片不应触发视频损坏判定，实际: %+v", issues)
	}
}

func TestClassifyMediaIssues_NoThumbnail(t *testing.T) {
	checks := healthChecks()
	checks.thumbnail = func(string) error { return errors.New("缩略图生成失败") }
	mf := models.MediaFile{ID: 1, FilePath: "D:/v/a.mp4", FileSize: 100}
	issues := classifyMediaIssues(mf, checks)
	if len(issues) != 1 || issues[0].IssueType != models.HealthIssueNoThumbnail {
		t.Fatalf("应判定为缩略图无法生成，实际: %+v", issues)
	}
}

func TestClassifyMediaIssues_Healthy(t *testing.T) {
	mf := models.MediaFile{ID: 1, FilePath: "D:/v/ok.mp4", FileSize: 100}
	if issues := classifyMediaIssues(mf, healthChecks()); len(issues) != 0 {
		t.Fatalf("健康媒体不应有问题，实际: %+v", issues)
	}
}

// TestRunHealthScan_WritesIssuesAndNeverTouchesDeletedAt 巡检写入问题表，且绝不改 deleted_at。
func TestRunHealthScan_WritesIssuesAndNeverTouchesDeletedAt(t *testing.T) {
	gdb := newHealthTestDB(t)
	// 三条未软删媒体：一条 0 字节、一条 missing、一条健康
	gdb.Create(&models.MediaFile{ID: 1, LibraryID: 1, FilePath: "D:/v/zero.mp4", FileName: "zero.mp4", FileSize: 0})
	gdb.Create(&models.MediaFile{ID: 2, LibraryID: 1, FilePath: "D:/v/gone.mp4", FileName: "gone.mp4", FileSize: 100})
	gdb.Create(&models.MediaFile{ID: 3, LibraryID: 1, FilePath: "D:/v/ok.mp4", FileName: "ok.mp4", FileSize: 100})
	// 一条已软删媒体：巡检应跳过、且 deleted_at 不被改
	softDeleted := time.Now().Add(-time.Hour)
	gdb.Create(&models.MediaFile{ID: 4, LibraryID: 1, FilePath: "D:/v/del.mp4", FileName: "del.mp4", FileSize: 0, DeletedAt: &softDeleted})

	checks := healthChecks()
	checks.stat = func(path string) (os.FileInfo, error) {
		if path == "D:/v/gone.mp4" {
			return nil, os.ErrNotExist
		}
		return nil, nil
	}
	svc := NewHealthService(gdb, checks)

	if err := svc.runScan(); err != nil {
		t.Fatalf("巡检执行失败: %v", err)
	}

	// 问题表：id=1 zero_byte、id=2 missing，共 2 条
	var issues []models.MediaHealthIssue
	gdb.Order("media_id ASC").Find(&issues)
	if len(issues) != 2 {
		t.Fatalf("应写入 2 条问题，实际 %d 条: %+v", len(issues), issues)
	}
	if issues[0].MediaID != 1 || issues[0].IssueType != models.HealthIssueZeroByte {
		t.Fatalf("首条问题应为 id=1 zero_byte，实际: %+v", issues[0])
	}
	if issues[1].MediaID != 2 || issues[1].IssueType != models.HealthIssueMissing {
		t.Fatalf("次条问题应为 id=2 missing，实际: %+v", issues[1])
	}

	// 关键：巡检绝不改任何 media_files.deleted_at
	var all []models.MediaFile
	gdb.Find(&all)
	for _, mf := range all {
		switch mf.ID {
		case 4:
			if mf.DeletedAt == nil {
				t.Fatalf("已软删媒体 id=4 的 deleted_at 不应被清空")
			}
		default:
			if mf.DeletedAt != nil {
				t.Fatalf("未软删媒体 id=%d 的 deleted_at 被巡检误写: %v", mf.ID, mf.DeletedAt)
			}
		}
	}

	status := svc.Status()
	if status.Status != healthStatusCompleted {
		t.Fatalf("巡检后状态应为 completed，实际: %s", status.Status)
	}
	if status.IssueCount != 2 || status.Total != 3 {
		t.Fatalf("状态计数不符：total=%d issue=%d", status.Total, status.IssueCount)
	}
}

// TestRunHealthScan_ClearsPreviousIssues 每轮巡检先清空旧问题再写入当轮快照。
func TestRunHealthScan_ClearsPreviousIssues(t *testing.T) {
	gdb := newHealthTestDB(t)
	gdb.Create(&models.MediaFile{ID: 1, LibraryID: 1, FilePath: "D:/v/a.mp4", FileName: "a.mp4", FileSize: 100})
	// 预置一条陈旧问题（上一轮残留）
	gdb.Create(&models.MediaHealthIssue{SpaceID: models.DefaultSpaceID, MediaID: 999, IssueType: models.HealthIssueBroken, CheckedAt: time.Now().Add(-time.Hour)})

	svc := NewHealthService(gdb, healthChecks()) // 全部健康
	if err := svc.runScan(); err != nil {
		t.Fatalf("巡检执行失败: %v", err)
	}

	var cnt int64
	gdb.Model(&models.MediaHealthIssue{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("健康库巡检后问题表应清空，实际 %d 条", cnt)
	}
}

func TestTryGenerateThumbnailMissingFile(t *testing.T) {
	// 缩略图同步生成入口：对不存在的文件应返回错误（不静默吞）
	err := TryGenerateThumbnail(filepath.Join(t.TempDir(), "nope.mp4"))
	if err == nil {
		t.Fatal("对不存在文件应返回错误")
	}
}
