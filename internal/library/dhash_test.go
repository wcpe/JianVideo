package library

import (
	"image"
	"image/color"
	"testing"
)

// makeHTentImage 构造水平「帐篷」亮度图：每行亮度先随 x 升后降（中间最亮）。
// 相邻像素在左半段递增（左<右→位 0）、右半段递减（左>右→位 1），
// 产生约一半置位的结构化 dHash，且对不同分辨率稳定（适合可重复断言）。
func makeHTentImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// 三角波：0 → 255 → 0
			var v uint8
			if x < w/2 {
				v = uint8(x * 255 / (w / 2))
			} else {
				v = uint8((w - 1 - x) * 255 / (w / 2))
			}
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// makeVTentImage 构造竖直「帐篷」亮度图：亮度随 y 先升后降，每行内为常数。
// 同行相邻像素恒相等（左>右恒为否）→ dHash 全 0，与水平帐篷结构迥异。
func makeVTentImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		var v uint8
		if y < h/2 {
			v = uint8(y * 255 / (h / 2))
		} else {
			v = uint8((h - 1 - y) * 255 / (h / 2))
		}
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// TestComputeDHash_Stable 同一图像两次计算结果一致、与自身距离为 0。
func TestComputeDHash_Stable(t *testing.T) {
	img := makeHTentImage(64, 64)
	h1 := computeDHash(img)
	h2 := computeDHash(img)
	if h1 != h2 {
		t.Fatalf("同一图像两次 dHash 不一致: %d != %d", h1, h2)
	}
	if d := hammingDistance(h1, h2); d != 0 {
		t.Fatalf("自身汉明距离应为 0, 实际 %d", d)
	}
	if h1 == 0 {
		t.Fatal("结构化图像 dHash 不应为 0（说明未捕获结构）")
	}
}

// TestComputeDHash_SimilarSmallDistance 同款帐篷图换分辨率后 dHash 距离应很小（≤ 阈值）。
func TestComputeDHash_SimilarSmallDistance(t *testing.T) {
	a := computeDHash(makeHTentImage(64, 64))
	// 不同分辨率的同款结构，dHash 应高度相似
	b := computeDHash(makeHTentImage(128, 96))
	d := hammingDistance(a, b)
	if d > dedupHammingThreshold {
		t.Fatalf("相似图像汉明距离应 ≤ %d, 实际 %d", dedupHammingThreshold, d)
	}
}

// TestComputeDHash_DifferentLargeDistance 水平帐篷与竖直帐篷结构迥异，距离应大于阈值。
func TestComputeDHash_DifferentLargeDistance(t *testing.T) {
	a := computeDHash(makeHTentImage(64, 64))
	b := computeDHash(makeVTentImage(64, 64))
	d := hammingDistance(a, b)
	if d <= dedupHammingThreshold {
		t.Fatalf("迥异图像汉明距离应 > %d, 实际 %d", dedupHammingThreshold, d)
	}
}

// TestHammingDistance 验证汉明距离基本性质。
func TestHammingDistance(t *testing.T) {
	cases := []struct {
		a, b uint64
		want int
	}{
		{0, 0, 0},
		{0, 1, 1},
		{0b1011, 0b0001, 2},
		{0xFFFFFFFFFFFFFFFF, 0, 64},
	}
	for _, c := range cases {
		if got := hammingDistance(c.a, c.b); got != c.want {
			t.Errorf("hammingDistance(%d, %d) = %d, 期望 %d", c.a, c.b, got, c.want)
		}
	}
}

// TestClusterByHamming_GroupsSimilar 距离 ≤ 阈值的项聚为一组，单成员不成组。
func TestClusterByHamming_GroupsSimilar(t *testing.T) {
	// id=1/2 完全相同 → 同组；id=3 与前两者距离极大 → 不与它们成组、自身单独不返回。
	items := []dhashItem{
		{id: 1, hash: 0x0F0F0F0F0F0F0F0F},
		{id: 2, hash: 0x0F0F0F0F0F0F0F0F},
		{id: 3, hash: 0xFFFFFFFFFFFFFFFF},
	}
	groups := clusterByHamming(items, dedupHammingThreshold)
	if len(groups) != 1 {
		t.Fatalf("应只返回 1 个重复组, 实际 %d 组: %v", len(groups), groups)
	}
	if len(groups[0]) != 2 || groups[0][0] != 1 || groups[0][1] != 2 {
		t.Fatalf("重复组应为 [1 2]（组内 id 升序）, 实际 %v", groups[0])
	}
}

// TestClusterByHamming_Transitive 链式相似（1~2、2~3 都近似）应并入同一组。
func TestClusterByHamming_Transitive(t *testing.T) {
	// 三个哈希两两相差 1 位，链式相连应聚成一组（并查集语义）。
	items := []dhashItem{
		{id: 10, hash: 0x0000000000000000},
		{id: 20, hash: 0x0000000000000001},
		{id: 30, hash: 0x0000000000000003},
	}
	groups := clusterByHamming(items, 2)
	if len(groups) != 1 {
		t.Fatalf("链式相似应聚成 1 组, 实际 %d 组", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Fatalf("组内应含 3 个成员, 实际 %v", groups[0])
	}
}

// TestClusterByHamming_NoDuplicates 全部互不相似时不返回任何组。
func TestClusterByHamming_NoDuplicates(t *testing.T) {
	items := []dhashItem{
		{id: 1, hash: 0x0000000000000000},
		{id: 2, hash: 0xFFFFFFFFFFFFFFFF},
	}
	groups := clusterByHamming(items, dedupHammingThreshold)
	if len(groups) != 0 {
		t.Fatalf("无相似项时应返回 0 组, 实际 %d 组", len(groups))
	}
}

// TestClusterByHamming_StableOrdering 组内按 id 升序、组间按首成员 id 升序，保证可测稳定。
func TestClusterByHamming_StableOrdering(t *testing.T) {
	items := []dhashItem{
		{id: 5, hash: 0xAAAAAAAAAAAAAAAA},
		{id: 1, hash: 0xAAAAAAAAAAAAAAAA},
		{id: 9, hash: 0x1111111111111111},
		{id: 4, hash: 0x1111111111111111},
	}
	groups := clusterByHamming(items, dedupHammingThreshold)
	if len(groups) != 2 {
		t.Fatalf("应返回 2 个重复组, 实际 %d", len(groups))
	}
	// 组内升序
	if groups[0][0] != 1 || groups[0][1] != 5 {
		t.Fatalf("第一组应为 [1 5], 实际 %v", groups[0])
	}
	if groups[1][0] != 4 || groups[1][1] != 9 {
		t.Fatalf("第二组应为 [4 9], 实际 %v", groups[1])
	}
	// 组间按首成员 id 升序：第一组首 id(1) < 第二组首 id(4)
	if groups[0][0] >= groups[1][0] {
		t.Fatalf("组间应按首成员 id 升序, 实际 %v", groups)
	}
}
