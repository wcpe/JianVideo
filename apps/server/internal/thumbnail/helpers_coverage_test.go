package thumbnail

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// TestCoverPathForAndHelpers 覆盖封面路径与尺寸/检查点助手。
func TestCoverPathForAndHelpers(t *testing.T) {
	t.Parallel()

	fp := strings.Repeat("ab", 16) // 32 hex
	path, err := CoverPathFor(`D:\data`, models.DefaultSpaceID, 9, fp)
	if err != nil {
		t.Fatalf("CoverPathFor: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(path), "/covers/") || !strings.HasSuffix(path, fp+".jpg") {
		t.Fatalf("路径异常: %s", path)
	}
	if _, err := CoverPathFor("data", "../evil", 1, fp); err == nil {
		t.Fatal("非法 space 应失败")
	}
	if _, err := CoverPathFor("data", models.DefaultSpaceID, 0, fp); err == nil {
		t.Fatal("mediaID<=0 应失败")
	}
	if _, err := CoverPathFor("data", models.DefaultSpaceID, 1, "short"); err == nil {
		t.Fatal("非法 fingerprint 应失败")
	}

	if err := gormRecordNotFound(); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("gormRecordNotFound: %v", err)
	}

	if id, err := parseCheckpoint(""); err != nil || id != 0 {
		t.Fatalf("空检查点: %v %d", err, id)
	}
	if id, err := parseCheckpoint("12"); err != nil || id != 12 {
		t.Fatalf("合法检查点: %v %d", err, id)
	}
	if _, err := parseCheckpoint("-1"); err == nil {
		t.Fatal("负检查点应失败")
	}
	if _, err := parseCheckpoint("x"); err == nil {
		t.Fatal("非法检查点应失败")
	}

	if normalizeSpace("") != models.DefaultSpaceID || normalizeSpace("  s1 ") != "s1" {
		t.Fatal("normalizeSpace")
	}
	if joinSizes([]int{320, 640}) != "320,640" {
		t.Fatal("joinSizes")
	}
	if generateKey("", 3, []int{320}) != "thumbnail:generate:"+models.DefaultSpaceID+":3:320" {
		t.Fatal("generateKey")
	}
	if !strings.HasPrefix(backfillKey("s1", []int{160}), "thumbnail:backfill:s1:") {
		t.Fatal("backfillKey")
	}

	sizes, err := normalizeSizes(nil, false)
	if err != nil || len(sizes) != 1 || sizes[0] != 320 {
		t.Fatalf("默认 sizes: %v %v", sizes, err)
	}
	all, err := normalizeSizes(nil, true)
	if err != nil || len(all) != len(library.SupportedThumbnailSizes()) {
		t.Fatalf("allByDefault: %v %v", all, err)
	}
	if _, err := normalizeSizes([]int{99999}, false); err == nil {
		t.Fatal("非法尺寸应失败")
	}
	dedup, err := normalizeSizes([]int{640, 320, 640}, false)
	if err != nil || len(dedup) != 2 || dedup[0] != 320 || dedup[1] != 640 {
		t.Fatalf("去重排序: %v %v", dedup, err)
	}

	if backfillProgress(0, 0) != 90 {
		t.Fatal("total<=0 进度")
	}
	if p := backfillProgress(50, 100); p < 5 || p > 90 {
		t.Fatalf("进度越界: %d", p)
	}
}
