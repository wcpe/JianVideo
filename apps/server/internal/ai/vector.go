package ai

import (
	"encoding/binary"
	"math"
)

// EncodeVector float32 小端序列化。
func EncodeVector(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeVector 反序列化。
func DecodeVector(b []byte) []float32 {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// CosineSimilarity 余弦相似度；维度不一致或零向量返回 0。
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// StubEmbedText 确定性 stub 嵌入：固定 dim=8，由文本字节哈希填充（可测必中）。
func StubEmbedText(text string, dim int) []float32 {
	if dim <= 0 {
		dim = 8
	}
	v := make([]float32, dim)
	if text == "" {
		v[0] = 1
		return v
	}
	var h uint32 = 2166136261
	for i := 0; i < len(text); i++ {
		h ^= uint32(text[i])
		h *= 16777619
		v[i%dim] += float32(h%1000) / 1000
	}
	// L2 归一化
	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	if norm > 0 {
		inv := float32(1 / math.Sqrt(norm))
		for i := range v {
			v[i] *= inv
		}
	}
	return v
}
