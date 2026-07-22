package library

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// setTestThumbnailDir 直接设置包级缩略图目录到测试临时目录，绕过 InitThumbnailDir 的 sync.Once，
// 使每个测试用例都有独立、存在的缩略图目录（测试串行执行，复用全局变量安全），结束后还原。
func setTestThumbnailDir(t *testing.T) {
	t.Helper()
	orig := thumbnailDir
	t.Cleanup(func() { thumbnailDir = orig })
	thumbnailDir = t.TempDir()
}

// writeThumbnailJPEG 在缩略图目录写入一张指定路径媒体对应的缩略图 JPEG，
// 模拟缩略图已惰性生成的情形，供去重扫描读取计算 dHash。
func writeThumbnailJPEG(t *testing.T, filePath string, img image.Image) {
	t.Helper()
	out := getThumbnailPath(filePath)
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("创建缩略图文件失败: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("编码缩略图失败: %v", err)
	}
}

// descendThumb 生成每行亮度从左到右单调递减的缩略图：相邻像素恒「左>右」→ dHash 全 1（非 0），
// 与水平帐篷结构迥异、不会聚为同组，且非 0 便于校验幂等（避免 0=未计算 的哨兵歧义）。
func descendThumb() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			v := uint8((31 - x) * 255 / 31)
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// hTentThumb 生成水平帐篷缩略图（每行先升后降），dHash 非 0 且对相同结构稳定相等。
func hTentThumb() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			var v uint8
			if x < 16 {
				v = uint8(x * 255 / 16)
			} else {
				v = uint8((31 - x) * 255 / 16)
			}
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// TestComputeMissingDHashes 为缺哈希的未软删媒体计算并持久化 dHash，二次调用幂等返回 0。
func TestComputeMissingDHashes(t *testing.T) {
	svc, gdb := newTestService(t)
	setTestThumbnailDir(t)

	// 两张结构一致的水平帐篷图（近似重复），一张单调递减图（迥异、dHash 非 0）
	mkMedia := func(name string, img image.Image) int64 {
		mf, err := svc.CreateMediaFile(1, filepath.ToSlash(filepath.Join("D:/imgs", name)), 100)
		if err != nil {
			t.Fatalf("入库失败: %v", err)
		}
		writeThumbnailJPEG(t, mf.FilePath, img)
		return mf.ID
	}
	id1 := mkMedia("a.jpg", hTentThumb())
	id2 := mkMedia("b.jpg", hTentThumb())
	_ = mkMedia("c.jpg", descendThumb())

	computed, err := svc.ComputeMissingDHashes()
	if err != nil {
		t.Fatalf("计算 dHash 失败: %v", err)
	}
	if computed != 3 {
		t.Fatalf("应计算 3 条 dHash, 实际 %d", computed)
	}

	// 落库校验：dhash 非 0，且两张相同图 dHash 相等
	var a, b models.MediaFile
	if err := gdb.First(&a, id1).Error; err != nil {
		t.Fatalf("查 a 失败: %v", err)
	}
	if err := gdb.First(&b, id2).Error; err != nil {
		t.Fatalf("查 b 失败: %v", err)
	}
	if a.DHash == 0 {
		t.Fatal("a 的 dhash 不应为 0")
	}
	if a.DHash != b.DHash {
		t.Fatalf("两张相同图 dHash 应相等: %d != %d", a.DHash, b.DHash)
	}

	// 二次调用幂等：无缺哈希项，computed=0
	again, err := svc.ComputeMissingDHashes()
	if err != nil {
		t.Fatalf("二次计算失败: %v", err)
	}
	if again != 0 {
		t.Fatalf("二次调用应为幂等(computed=0), 实际 %d", again)
	}
}

// TestFindDuplicateGroups 相似媒体聚为重复组，迥异项不入组，软删项排除。
func TestFindDuplicateGroups(t *testing.T) {
	svc, gdb := newTestService(t)
	setTestThumbnailDir(t)

	mkMedia := func(name string, img image.Image) int64 {
		mf, err := svc.CreateMediaFile(1, filepath.ToSlash(filepath.Join("D:/imgs", name)), 100)
		if err != nil {
			t.Fatalf("入库失败: %v", err)
		}
		writeThumbnailJPEG(t, mf.FilePath, img)
		return mf.ID
	}
	id1 := mkMedia("a.jpg", hTentThumb())
	id2 := mkMedia("b.jpg", hTentThumb())
	_ = mkMedia("c.jpg", descendThumb()) // 迥异，不应入任何组
	dupSoftDel := mkMedia("d.jpg", hTentThumb())

	if _, err := svc.ComputeMissingDHashes(); err != nil {
		t.Fatalf("计算 dHash 失败: %v", err)
	}

	// 软删第 4 张：它与 a/b 相似，但软删后必须从重复组排除
	if err := svc.DeleteMediaFile(dupSoftDel); err != nil {
		t.Fatalf("软删失败: %v", err)
	}

	groups, err := svc.FindDuplicateGroups(dedupHammingThreshold)
	if err != nil {
		t.Fatalf("查重复组失败: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("应只有 1 个重复组, 实际 %d", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Fatalf("组内应为 2 项(排除软删项), 实际 %d", len(groups[0]))
	}
	gotIDs := []int64{groups[0][0].ID, groups[0][1].ID}
	if gotIDs[0] != id1 || gotIDs[1] != id2 {
		t.Fatalf("重复组应为 [%d %d], 实际 %v", id1, id2, gotIDs)
	}

	// 确认软删项确实未被纳入（防回归）
	for _, m := range groups[0] {
		if m.ID == dupSoftDel {
			t.Fatal("软删项不应出现在重复组中")
		}
	}
	_ = gdb
}
