package dblog

import (
	"context"
	"testing"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// recordingWriter 记录每次 GORM logger 实际打印的内容，供断言「是否输出」。
type recordingWriter struct {
	lines []string
}

func (w *recordingWriter) Printf(format string, args ...interface{}) {
	w.lines = append(w.lines, format)
}

// newTestLogger 用记录型 writer 构造可切换 logger，便于断言不同级别下是否真打印。
func newTestLogger() (*Logger, *recordingWriter) {
	w := &recordingWriter{}
	return New(w), w
}

// TestNew_DefaultSilent 默认（未开启调试）应为安静级别：
// Info / Trace 普通 SQL 均不输出，避免刷屏。
func TestNew_DefaultSilent(t *testing.T) {
	l, w := newTestLogger()

	l.Info(context.Background(), "select 1")
	l.Trace(context.Background(), time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)

	if len(w.lines) != 0 {
		t.Fatalf("默认安静级别不应输出任何日志，实际输出 %d 条: %v", len(w.lines), w.lines)
	}
}

// TestSetEnabled_TogglesLevel 开启调试后应输出详细日志（Info 级），关闭后恢复安静。
func TestSetEnabled_TogglesLevel(t *testing.T) {
	l, w := newTestLogger()

	// 开启 → Info / Trace 普通 SQL 均输出。
	l.SetEnabled(true)
	l.Info(context.Background(), "info msg")
	l.Trace(context.Background(), time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)
	if len(w.lines) == 0 {
		t.Fatalf("开启调试后应输出详细日志，实际无输出")
	}

	// 关闭 → 恢复安静，不再输出。
	w.lines = nil
	l.SetEnabled(false)
	l.Info(context.Background(), "info msg")
	l.Trace(context.Background(), time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)
	if len(w.lines) != 0 {
		t.Fatalf("关闭调试后应恢复安静，实际仍输出 %d 条: %v", len(w.lines), w.lines)
	}
}

// TestEnabled_ReflectsState Enabled() 应如实反映当前开关状态。
func TestEnabled_ReflectsState(t *testing.T) {
	l, _ := newTestLogger()
	if l.Enabled() {
		t.Fatalf("默认应为关闭")
	}
	l.SetEnabled(true)
	if !l.Enabled() {
		t.Fatalf("开启后 Enabled() 应为 true")
	}
}

// TestLogMode_KeepsAtomicLevel GORM 启动时可能调用 LogMode 重设级别，
// 包装器应忽略该调用、以自身原子级别为唯一真源（仍返回安静）。
func TestLogMode_KeepsAtomicLevel(t *testing.T) {
	l, w := newTestLogger()
	// 模拟 GORM 用 Info 调 LogMode，本包应忽略，保持安静。
	got := l.LogMode(gormlogger.Info)
	got.Info(context.Background(), "should be silent")
	if len(w.lines) != 0 {
		t.Fatalf("LogMode 不应改变安静级别，实际输出 %d 条", len(w.lines))
	}
}
