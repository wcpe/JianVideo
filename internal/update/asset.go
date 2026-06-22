package update

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// checksumsFileName 校验和清单资产名（由 CI 发布，见 ADR-0032）。
const checksumsFileName = "checksums.txt"

// selectBinaryAsset 选与当前 GOOS/GOARCH 匹配的二进制资产。
// 命名约定：jianvideo-<goos>-<arch>[.exe]（见 build.yml）。排除校验和与 .sha256 文件。
func selectBinaryAsset(assets []Asset, goos, goarch string) *Asset {
	want := goos + "-" + goarch
	for i := range assets {
		n := assets[i].Name
		if n == checksumsFileName || strings.HasSuffix(n, ".sha256") {
			continue
		}
		if strings.Contains(n, want) {
			return &assets[i]
		}
	}
	return nil
}

// findChecksumsAsset 找 checksums.txt 资产。
func findChecksumsAsset(assets []Asset) *Asset {
	for i := range assets {
		if assets[i].Name == checksumsFileName {
			return &assets[i]
		}
	}
	return nil
}

// parseChecksums 解析 "<sha256>  <filename>" 格式（sha256sum 输出）为 文件名→十六进制摘要 映射。
func parseChecksums(content string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		// 兼容 "*filename"（二进制模式）前缀
		name := strings.TrimPrefix(fields[1], "*")
		out[name] = strings.ToLower(fields[0])
	}
	return out
}

// fileSHA256 计算文件的 sha256 十六进制摘要。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyChecksum 校验 path 文件的 sha256 是否等于清单中 assetName 对应摘要。
// 清单缺该项或摘要不符均返回 false。
func verifyChecksum(path, assetName, checksums string) (bool, error) {
	want, ok := parseChecksums(checksums)[assetName]
	if !ok {
		return false, nil
	}
	got, err := fileSHA256(path)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(got, want), nil
}
