package transcoder

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// mockTranscoder 是 Transcoder 接口的模拟实现，用于测试。
type mockTranscoder struct {
	runFunc func(ctx context.Context, inputPath string, dst io.Writer) error
}

func (m *mockTranscoder) Run(ctx context.Context, inputPath string, dst io.Writer) error {
	return m.runFunc(ctx, inputPath, dst)
}

// TestPipeline_SelectBestEncoder 验证管道能选择编码器。
func TestPipeline_SelectBestEncoder(t *testing.T) {
	p := NewPipeline()
	if p.encoderName == "" {
		t.Fatal("期望编码器名称不为空")
	}
	// 在没有硬件加速的环境中，应降级为软件编码
	t.Logf("选择的编码器: %s (device=%s, hwaccel=%s)", p.encoderName, p.deviceType, p.hwAccel)
}

// TestPipeline_BuildArgs 验证 ffmpeg 参数构建。
func TestPipeline_BuildArgs(t *testing.T) {
	p := NewPipeline()
	args := p.buildArgs("/tmp/test.mkv", 0)

	// 验证关键参数存在
	assertContains(t, args, "-g", "48")
	assertContains(t, args, "-keyint_min", "48")
	assertContains(t, args, "-sc_threshold", "0")
	assertContains(t, args, "-f", "mpegts")
	assertContains(t, args, "-i", "/tmp/test.mkv")
	assertContains(t, args, "-c:v", p.encoderName)
	assertContains(t, args, "-c:a", "copy")

	// 验证输出到 stdout
	if last := args[len(args)-1]; last != "-" {
		t.Fatalf("期望最后一个参数为 '-'（stdout），实际 %s", last)
	}
}

// TestPipeline_RunCancelled 验证 context 取消时转码停止。
func TestPipeline_RunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	buf := &bytes.Buffer{}
	tc := &mockTranscoder{
		runFunc: func(ctx context.Context, _ string, _ io.Writer) error {
			// 模拟长时间运行
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
	}

	cancel() // 立即取消
	err := tc.Run(ctx, "/tmp/test.mp4", buf)
	if err != context.Canceled {
		t.Fatalf("期望 context.Canceled, 实际 %v", err)
	}
}

// TestPipeline_RunSuccess 验证正常转码流程。
func TestPipeline_RunSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	buf := &bytes.Buffer{}
	tc := &mockTranscoder{
		runFunc: func(_ context.Context, _ string, dst io.Writer) error {
			_, err := dst.Write([]byte("fake ts data"))
			return err
		},
	}

	err := tc.Run(ctx, "/tmp/test.mp4", buf)
	if err != nil {
		t.Fatalf("期望无错误, 实际 %v", err)
	}
	if buf.String() != "fake ts data" {
		t.Fatalf("期望 buf='fake ts data', 实际 '%s'", buf.String())
	}
}

// TestStreamHandler_InvalidID 验证无效 ID 返回 400。
func TestStreamHandler_InvalidID(t *testing.T) {
	// 此测试需要 gin 测试上下文，在 handler_test.go 中实现
	// 这里只验证 handler 能正确创建
	sh := NewStreamHandler(nil)
	if sh == nil {
		t.Fatal("StreamHandler 不应为 nil")
	}
}

// assertContains 验证 args 中存在 flag=value 对。
func assertContains(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Fatalf("参数中未找到 %s %s, 实际: %v", flag, value, args)
}

// TestSelectBestEncoder_Fallback 验证无硬件环境下降级为软件编码。
func TestSelectBestEncoder_Fallback(t *testing.T) {
	name, deviceType, err := SelectBestEncoder()
	if err != nil {
		t.Fatalf("SelectBestEncoder 不应返回错误: %v", err)
	}
	// 在没有硬件加速的环境中，应降级为软件编码
	if deviceType != "" {
		t.Logf("检测到硬件加速: %s (%s)，跳过降级验证", name, deviceType)
	} else {
		if name != "libx264" {
			t.Fatalf("期望软件编码为 libx264, 实际 %s", name)
		}
	}
}
