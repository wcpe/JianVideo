package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestSelectBinaryAsset 校验按平台选二进制资产，排除校验和与 .sha256。
func TestSelectBinaryAsset(t *testing.T) {
	assets := []Asset{
		{Name: "checksums.txt"},
		{Name: "jianvideo-linux-amd64"},
		{Name: "jianvideo-linux-amd64.sha256"},
		{Name: "jianvideo-windows-amd64.exe"},
		{Name: "jianvideo-windows-amd64.sha256"},
	}
	if a := selectBinaryAsset(assets, "linux", "amd64"); a == nil || a.Name != "jianvideo-linux-amd64" {
		t.Errorf("linux/amd64 应选 jianvideo-linux-amd64，得到 %+v", a)
	}
	if a := selectBinaryAsset(assets, "windows", "amd64"); a == nil || a.Name != "jianvideo-windows-amd64.exe" {
		t.Errorf("windows/amd64 应选 .exe，得到 %+v", a)
	}
	if a := selectBinaryAsset(assets, "darwin", "arm64"); a != nil {
		t.Errorf("无匹配平台应返回 nil，得到 %+v", a)
	}
}

// TestFindChecksumsAsset 校验找到校验和清单。
func TestFindChecksumsAsset(t *testing.T) {
	if a := findChecksumsAsset([]Asset{{Name: "jianvideo-linux-amd64"}, {Name: "checksums.txt"}}); a == nil {
		t.Error("应找到 checksums.txt")
	}
	if a := findChecksumsAsset([]Asset{{Name: "jianvideo-linux-amd64"}}); a != nil {
		t.Error("无清单应返回 nil")
	}
}

// TestParseChecksumsAndVerify 校验清单解析与 sha256 比对（含篡改场景）。
func TestParseChecksumsAndVerify(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "jianvideo-linux-amd64")
	content := []byte("hello-binary-content")
	if err := os.WriteFile(bin, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])
	checksums := hexSum + "  jianvideo-linux-amd64\n" + "0000  other-file\n"

	ok, err := verifyChecksum(bin, "jianvideo-linux-amd64", checksums)
	if err != nil || !ok {
		t.Errorf("正确校验和应通过，ok=%v err=%v", ok, err)
	}
	// 篡改清单摘要 → 不通过
	bad := "deadbeef  jianvideo-linux-amd64\n"
	if ok, _ := verifyChecksum(bin, "jianvideo-linux-amd64", bad); ok {
		t.Error("错误校验和应不通过")
	}
	// 清单缺该项 → 不通过
	if ok, _ := verifyChecksum(bin, "jianvideo-linux-amd64", "0000  unrelated\n"); ok {
		t.Error("清单缺项应不通过")
	}
}
