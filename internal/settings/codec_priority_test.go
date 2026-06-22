package settings

import (
	"reflect"
	"testing"
)

// TestTranscodeCodecPriority_DefaultUnset 未配置时默认返回 ["h264"]。
func TestTranscodeCodecPriority_DefaultUnset(t *testing.T) {
	svc := NewService(setupTestDB(t))
	got := svc.TranscodeCodecPriority()
	if !reflect.DeepEqual(got, []string{"h264"}) {
		t.Fatalf("未配置应默认 [h264]，实际 %v", got)
	}
}

// TestTranscodeCodecPriority_RoundTrip 合法优先级写入后原样读回。
func TestTranscodeCodecPriority_RoundTrip(t *testing.T) {
	svc := NewService(setupTestDB(t))
	allowed := []string{"h264", "h265", "av1"}
	want := []string{"av1", "h265", "h264"}

	if err := svc.SetTranscodeCodecPriority(want, allowed); err != nil {
		t.Fatalf("写入合法优先级失败: %v", err)
	}
	got := svc.TranscodeCodecPriority()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip 不一致：写 %v 读 %v", want, got)
	}
}

// TestSetTranscodeCodecPriority_RejectUnsupported 含不在 allowed 集内的编码被整体拒绝。
func TestSetTranscodeCodecPriority_RejectUnsupported(t *testing.T) {
	svc := NewService(setupTestDB(t))
	allowed := []string{"h264"} // 系统只实测可输出 h264

	if err := svc.SetTranscodeCodecPriority([]string{"av1", "h264"}, allowed); err == nil {
		t.Fatal("含不支持编码 av1 应被拒绝")
	}
	// 拒绝后不得落库，读回仍为默认
	if got := svc.TranscodeCodecPriority(); !reflect.DeepEqual(got, []string{"h264"}) {
		t.Fatalf("拒绝后不应写库，期望默认 [h264]，实际 %v", got)
	}
}

// TestSetTranscodeCodecPriority_RejectUnknownCodec 含管道不识别的编码（即便在 allowed 误传）也被拒。
func TestSetTranscodeCodecPriority_RejectUnknownCodec(t *testing.T) {
	svc := NewService(setupTestDB(t))
	allowed := []string{"h264", "mpeg2"}

	if err := svc.SetTranscodeCodecPriority([]string{"mpeg2"}, allowed); err == nil {
		t.Fatal("管道不识别的编码 mpeg2 应被拒绝")
	}
}

// TestSetTranscodeCodecPriority_RejectEmpty 空优先级被拒。
func TestSetTranscodeCodecPriority_RejectEmpty(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if err := svc.SetTranscodeCodecPriority([]string{}, []string{"h264"}); err == nil {
		t.Fatal("空优先级应被拒绝")
	}
}

// TestSetTranscodeCodecPriority_RejectDuplicate 重复编码被拒（避免歧义配置）。
func TestSetTranscodeCodecPriority_RejectDuplicate(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if err := svc.SetTranscodeCodecPriority([]string{"h264", "h264"}, []string{"h264"}); err == nil {
		t.Fatal("重复编码应被拒绝")
	}
}

// TestTranscodeCodecPriority_CorruptValueFallback 库内值损坏时回落默认 [h264]。
func TestTranscodeCodecPriority_CorruptValueFallback(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if err := svc.Set(KeyTranscodeCodecPriority, "not-json"); err != nil {
		t.Fatalf("写入损坏值失败: %v", err)
	}
	got := svc.TranscodeCodecPriority()
	if !reflect.DeepEqual(got, []string{"h264"}) {
		t.Fatalf("损坏值应回落 [h264]，实际 %v", got)
	}
}
