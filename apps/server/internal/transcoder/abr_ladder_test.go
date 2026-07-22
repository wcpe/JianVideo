package transcoder

import (
	"strings"
	"testing"
)

func TestABRLadderForSourceFiltersHigherVariants(t *testing.T) {
	ladder, err := ABRLadderForSource(1280, 720, []string{"1080p", "720p", "480p"})
	if err != nil {
		t.Fatalf("裁剪 ABR ladder 失败: %v", err)
	}
	if got := abrVariantNames(ladder); strings.Join(got, ",") != "720p,480p" {
		t.Fatalf("720p 源档位异常: %v", got)
	}
}

func TestABRLadderForSourceUsesSourceWithoutUpscaleBelow480p(t *testing.T) {
	ladder, err := ABRLadderForSource(640, 360, nil)
	if err != nil {
		t.Fatalf("生成低分辨率 ABR ladder 失败: %v", err)
	}
	if len(ladder) != 1 {
		t.Fatalf("低分辨率源应只有 source 档，实际 %+v", ladder)
	}
	variant := ladder[0]
	if variant.Name != "source" || variant.Width != 640 || variant.Height != 360 {
		t.Fatalf("低分辨率源不得放大: %+v", variant)
	}
}

func TestABRLadderForSourceRejectsUnknownVariant(t *testing.T) {
	if _, err := ABRLadderForSource(1920, 1080, []string{"1080p", "未知档位"}); err == nil {
		t.Fatal("未知 ABR 档位应被拒绝")
	}
}

func TestBuildABRArgsAndMasterUseVariantDirectories(t *testing.T) {
	pipeline := &Pipeline{encoderName: "libx264"}
	multi := NewMultiPipeline(pipeline)
	ladder, err := ABRLadderForSource(1280, 720, nil)
	if err != nil {
		t.Fatalf("生成 ABR ladder 失败: %v", err)
	}
	args := multi.BuildABRArgs("D:/video/source.mp4", ladder)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"split=2",
		"[v1]scale=1280:720,format=yuv420p[v1out]",
		"[v2]scale=854:480,format=yuv420p[v2out]",
		"720p/segment_%03d.ts",
		"720p/index.m3u8",
		"480p/segment_%03d.ts",
		"480p/index.m3u8",
		"0:a:0?",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ABR 参数缺少 %q: %v", want, args)
		}
	}

	master := BuildABRMasterM3U8(ladder)
	if strings.Count(master, "#EXT-X-STREAM-INF") != 2 {
		t.Fatalf("master 应包含两个档位: %s", master)
	}
	for _, want := range []string{"RESOLUTION=1280x720", "720p/index.m3u8", "RESOLUTION=854x480", "480p/index.m3u8"} {
		if !strings.Contains(master, want) {
			t.Fatalf("master 缺少 %q: %s", want, master)
		}
	}
}

func abrVariantNames(ladder []QualityDefinition) []string {
	names := make([]string, len(ladder))
	for i := range ladder {
		names[i] = ladder[i].Name
	}
	return names
}
