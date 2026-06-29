package library

import (
	"fmt"
	"image"
	"image/jpeg"
	"math/bits"
	"os"
	"sort"
)

// 感知哈希去重（FR-70）：基于缩略图计算 64 位差分哈希（dHash），
// 以汉明距离聚类近似重复媒体。纯 Go 标准库实现，不引入第三方图像哈希库。

const (
	// dHashWidth 差分哈希缩放后的宽度（比高度多 1 列，用于逐行相邻差分得 8 位）。
	dHashWidth = 9
	// dHashHeight 差分哈希缩放后的高度，8 行 × 8 差分位 = 64 位。
	dHashHeight = 8
	// dedupHammingThreshold 判定近似重复的汉明距离阈值（经验值）：
	// 64 位中约 16% 不同仍视为相似，定义为常量便于后续调整。
	dedupHammingThreshold = 10
)

// dhashItem 参与聚类的单个媒体哈希条目。
type dhashItem struct {
	id   int64
	hash uint64
}

// computeDHash 计算图像的 64 位差分哈希。
// 步骤：最近邻缩放到 9×8 → 转灰度 → 逐行比较相邻像素亮度（左 > 右 置 1），得 8×8=64 位。
// 缩放与灰度均用标准库实现，结果对轻微缩放 / 重编码稳定。
func computeDHash(img image.Image) uint64 {
	gray := resizeGrayNearest(img, dHashWidth, dHashHeight)
	var hash uint64
	bit := 0
	for y := 0; y < dHashHeight; y++ {
		for x := 0; x < dHashWidth-1; x++ {
			// 同行相邻像素：左侧更亮置 1，否则置 0
			if gray[y*dHashWidth+x] > gray[y*dHashWidth+x+1] {
				hash |= 1 << uint(bit)
			}
			bit++
		}
	}
	return hash
}

// resizeGrayNearest 用最近邻采样把图像缩放到 w×h 并转灰度，返回行优先的灰度值切片。
// 灰度按 ITU-R BT.601 加权（0.299R+0.587G+0.114B），用整数运算避免浮点误差。
func resizeGrayNearest(img image.Image, w, h int) []uint8 {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	out := make([]uint8, w*h)
	if srcW == 0 || srcH == 0 {
		return out
	}
	for y := 0; y < h; y++ {
		// 取目标像素中心反映射回源坐标，避免边缘偏移
		sy := b.Min.Y + (y*srcH+srcH/2)/h
		if sy >= b.Max.Y {
			sy = b.Max.Y - 1
		}
		for x := 0; x < w; x++ {
			sx := b.Min.X + (x*srcW+srcW/2)/w
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			// image 的 RGBA() 返回 16 位预乘分量，右移 8 位回到 0-255
			r, g, bl, _ := img.At(sx, sy).RGBA()
			r8, g8, b8 := r>>8, g>>8, bl>>8
			out[y*w+x] = uint8((299*r8 + 587*g8 + 114*b8) / 1000)
		}
	}
	return out
}

// hammingDistance 计算两个 64 位哈希的汉明距离（不同位的个数）。
func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// dHashFromThumbnail 读取缩略图 JPEG 并计算其 dHash。
// 缩略图统一为 320 宽 JPEG（见 thumbnail.go），标准库 image/jpeg 直接解码。
func dHashFromThumbnail(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("解码缩略图失败: %w", err)
	}
	return computeDHash(img), nil
}

// clusterByHamming 按汉明距离阈值把哈希条目聚类为重复组。
// 用并查集把距离 ≤ threshold 的条目连通；仅返回成员数 ≥ 2 的组。
// 组内按 id 升序、组间按首成员 id 升序，保证输出稳定可测。
func clusterByHamming(items []dhashItem, threshold int) [][]int64 {
	n := len(items)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]] // 路径压缩
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// 逐对比较：相似即并入同一连通分量
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if hammingDistance(items[i].hash, items[j].hash) <= threshold {
				union(i, j)
			}
		}
	}

	// 按根聚合各连通分量的成员 id
	groupsByRoot := make(map[int][]int64)
	for i := range items {
		root := find(i)
		groupsByRoot[root] = append(groupsByRoot[root], items[i].id)
	}

	// 仅保留 ≥2 成员的组，组内 id 升序
	groups := make([][]int64, 0, len(groupsByRoot))
	for _, ids := range groupsByRoot {
		if len(ids) < 2 {
			continue
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		groups = append(groups, ids)
	}
	// 组间按首成员 id 升序
	sort.Slice(groups, func(a, b int) bool { return groups[a][0] < groups[b][0] })
	return groups
}
