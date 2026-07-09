package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func writeHashTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestComputeFileContentHashStreamsSHA256(t *testing.T) {
	dir := t.TempDir()
	path := writeHashTestFile(t, dir, "a.bin", []byte("same-content"))

	got, err := ComputeFileContentHash(path)
	if err != nil {
		t.Fatalf("计算内容 hash 失败: %v", err)
	}
	if got.Algo != ContentHashAlgoSHA256 {
		t.Fatalf("算法应为 sha256，实际 %s", got.Algo)
	}
	if got.Hash != sha256Hex([]byte("same-content")) {
		t.Fatalf("hash 不匹配: %s", got.Hash)
	}
	if got.Size != int64(len("same-content")) {
		t.Fatalf("文件大小不匹配: %d", got.Size)
	}
}

func TestBackfillContentHashesAndFindExactDuplicateGroups(t *testing.T) {
	svc, gdb := newTestService(t)
	dir := t.TempDir()
	aPath := writeHashTestFile(t, dir, "a.mp4", []byte("same-content"))
	bPath := writeHashTestFile(t, dir, "b.mp4", []byte("same-content"))
	cPath := writeHashTestFile(t, dir, "c.mp4", []byte("other-content"))

	a, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, aPath, 0)
	b, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, bPath, 0)
	_, _ = svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, cPath, 0)

	result, err := svc.BackfillContentHashes(context.Background(), models.DefaultSpaceID, nil)
	if err != nil {
		t.Fatalf("回填内容 hash 失败: %v", err)
	}
	if result.Computed != 3 {
		t.Fatalf("应回填 3 条，实际 %+v", result)
	}

	var gotA models.MediaFile
	if err := gdb.First(&gotA, a.ID).Error; err != nil {
		t.Fatalf("查询 a 失败: %v", err)
	}
	if gotA.ContentHash == "" || gotA.ContentHashAlgo != ContentHashAlgoSHA256 || gotA.ContentHashStale {
		t.Fatalf("a 的内容 hash 字段未正确写入: %+v", gotA)
	}

	groups, err := svc.FindExactDuplicateGroupsInSpace(models.DefaultSpaceID)
	if err != nil {
		t.Fatalf("查询精确重复组失败: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("应只有一个 2 项精确重复组，实际 %+v", groups)
	}
	if groups[0].Items[0].ID != a.ID || groups[0].Items[1].ID != b.ID {
		t.Fatalf("重复组成员不正确: %+v", groups[0].Items)
	}
}

func TestBackfillContentHashesSkipsSMBFailure(t *testing.T) {
	svc, gdb := newTestService(t)
	dir := t.TempDir()
	localPath := writeHashTestFile(t, dir, "local.mp4", []byte("local-content"))
	_, _ = svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, localPath, 0)
	if err := gdb.Create(&models.MediaFile{
		SpaceID:          models.DefaultSpaceID,
		LibraryID:        1,
		FilePath:         "smb://nas/video/remote.mp4",
		FileName:         "remote.mp4",
		FileSize:         13,
		FileState:        models.MediaFileStateAvailable,
		ContentHashStale: true,
		AddedAt:          time.Now().UTC(),
		ModifiedAt:       time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("插入 SMB 测试媒体失败: %v", err)
	}

	result, err := svc.BackfillContentHashes(context.Background(), models.DefaultSpaceID, nil)
	if err != nil {
		t.Fatalf("部分失败不应阻断回填: %v", err)
	}
	if result.Computed != 1 || result.Failed != 1 {
		t.Fatalf("应回填本地文件并跳过 SMB 文件，实际 %+v", result)
	}
	var remote models.MediaFile
	if err := gdb.Where("file_path = ?", "smb://nas/video/remote.mp4").First(&remote).Error; err != nil {
		t.Fatalf("查询 SMB 媒体失败: %v", err)
	}
	if remote.ContentHash != "" || !remote.ContentHashStale {
		t.Fatalf("SMB 失败项不应写入内容 hash: %+v", remote)
	}
}

func TestFindExactDuplicateGroupsExcludesDifferentSpaceStaleMissingAndDeleted(t *testing.T) {
	svc, gdb := newTestService(t)
	hash := sha256Hex([]byte("same"))
	now := time.Now().UTC()
	rows := []models.MediaFile{
		{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/a.mp4", FileName: "a.mp4", FileSize: 4, ContentHash: hash, ContentHashAlgo: ContentHashAlgoSHA256, ContentHashComputedAt: &now, ContentHashStale: false, FileState: models.MediaFileStateAvailable},
		{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/b.mp4", FileName: "b.mp4", FileSize: 4, ContentHash: hash, ContentHashAlgo: ContentHashAlgoSHA256, ContentHashComputedAt: &now, ContentHashStale: false, FileState: models.MediaFileStateAvailable},
		{SpaceID: "space-other", LibraryID: 1, FilePath: "E:/c.mp4", FileName: "c.mp4", FileSize: 4, ContentHash: hash, ContentHashAlgo: ContentHashAlgoSHA256, ContentHashComputedAt: &now, ContentHashStale: false, FileState: models.MediaFileStateAvailable},
		{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/stale.mp4", FileName: "stale.mp4", FileSize: 4, ContentHash: hash, ContentHashAlgo: ContentHashAlgoSHA256, ContentHashComputedAt: &now, ContentHashStale: true, FileState: models.MediaFileStateAvailable},
		{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/missing.mp4", FileName: "missing.mp4", FileSize: 4, ContentHash: hash, ContentHashAlgo: ContentHashAlgoSHA256, ContentHashComputedAt: &now, ContentHashStale: false, FileState: models.MediaFileStateMissing},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("插入测试媒体失败: %v", err)
	}
	if err := gdb.Model(&models.MediaFile{}).
		Where("file_name IN ?", []string{"a.mp4", "b.mp4", "c.mp4", "missing.mp4"}).
		Update("content_hash_stale", false).Error; err != nil {
		t.Fatalf("更新测试媒体 hash 状态失败: %v", err)
	}
	deleted, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, "D:/deleted.mp4", 4)
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", deleted.ID).Updates(map[string]any{
		"content_hash":             hash,
		"content_hash_algo":        ContentHashAlgoSHA256,
		"content_hash_computed_at": now,
	}).Error; err != nil {
		t.Fatalf("写入软删前 hash 失败: %v", err)
	}
	if err := svc.DeleteMediaFileInSpace(models.DefaultSpaceID, deleted.ID); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	if err := svc.RefreshContentHashGroups(context.Background(), models.DefaultSpaceID); err != nil {
		t.Fatalf("刷新内容 hash 分组失败: %v", err)
	}

	groups, err := svc.FindExactDuplicateGroupsInSpace(models.DefaultSpaceID)
	if err != nil {
		t.Fatalf("查询精确重复组失败: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("应只包含当前 Space 两条可用媒体，实际 %+v", groups)
	}
}

func TestScanChangeMarksContentHashStaleWhenSizeOrMTimeChanges(t *testing.T) {
	svc, gdb := newTestService(t)
	dir := t.TempDir()
	path := writeHashTestFile(t, dir, "change.mp4", []byte("old"))
	mf, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, path, 3)
	now := time.Now().UTC()
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(map[string]any{
		"content_hash":             sha256Hex([]byte("old")),
		"content_hash_algo":        ContentHashAlgoSHA256,
		"content_hash_computed_at": now,
		"content_hash_stale":       false,
		"modified_at":              now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("预置 hash 失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("new-content"), 0o644); err != nil {
		t.Fatalf("修改文件失败: %v", err)
	}

	_, err := svc.ApplyScanChange(ScanChange{
		SpaceID:   models.DefaultSpaceID,
		LibraryID: 1,
		Path:      path,
		Op:        ScanChangeModified,
	})
	if err != nil {
		t.Fatalf("应用扫描变更失败: %v", err)
	}

	var got models.MediaFile
	if err := gdb.First(&got, mf.ID).Error; err != nil {
		t.Fatalf("查询媒体失败: %v", err)
	}
	if !got.ContentHashStale {
		t.Fatalf("文件变化后 content_hash_stale 应为 true: %+v", got)
	}
}
