// Package dblog 提供可运行时切换级别的 GORM 日志器（FR-110）。
//
// 默认安静（仅 Error 级，且忽略 record-not-found），不刷普通 SQL 与 record-not-found 噪音；
// 开启「调试日志」后切到 Info 级，输出 SQL 与慢查询详细日志。开关以原子变量持有，
// SetEnabled 在运行期即时切换、无需重建数据库连接，配合设置「保存即生效」路径使用。
//
// 本包无业务依赖，仅依赖 gorm logger，可被 main / api 单向依赖。
package dblog

import (
	"context"
	"log"
	"os"
	"sync/atomic"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// slowThreshold 慢查询阈值：开启调试时超过此耗时的 SQL 以 warn 级标记输出。
const slowThreshold = 200 * time.Millisecond

// Logger 包装「安静」与「详细」两个委托 gorm logger，按原子开关选择实际输出者。
// 安静态为 Error 级并忽略 record-not-found；开启态为 Info 级输出 SQL 与慢查询。
type Logger struct {
	silent  gormlogger.Interface
	verbose gormlogger.Interface
	// enabled 调试日志开关；true=详细日志，false=安静。以原子值保证并发安全无锁读。
	enabled atomic.Bool
}

// New 基于给定 writer 构造可切换日志器，初始为安静（关闭）。
// 安静态忽略 record-not-found，避免业务上常见的「查无记录」刷错误日志。
func New(writer gormlogger.Writer) *Logger {
	silent := gormlogger.New(writer, gormlogger.Config{
		LogLevel:                  gormlogger.Error,
		IgnoreRecordNotFoundError: true,
	})
	verbose := gormlogger.New(writer, gormlogger.Config{
		SlowThreshold:             slowThreshold,
		LogLevel:                  gormlogger.Info,
		IgnoreRecordNotFoundError: false,
	})
	return &Logger{silent: silent, verbose: verbose}
}

// NewDefault 使用进程标准输出构造默认 writer 的可切换日志器，供 main 注入 gorm.Config。
func NewDefault() *Logger {
	return New(log.New(os.Stdout, "\r\n", log.LstdFlags))
}

// SetEnabled 运行期切换调试日志开关，立即生效。
func (l *Logger) SetEnabled(on bool) {
	l.enabled.Store(on)
}

// Enabled 返回当前调试日志开关状态。
func (l *Logger) Enabled() bool {
	return l.enabled.Load()
}

// active 返回当前应使用的委托 logger：开启=详细，关闭=安静。
func (l *Logger) active() gormlogger.Interface {
	if l.enabled.Load() {
		return l.verbose
	}
	return l.silent
}

// LogMode 实现 gorm logger 接口。GORM 初始化时可能调用本方法重设级别，
// 本包以自身原子开关为唯一真源、忽略外部设定，返回自身。
func (l *Logger) LogMode(gormlogger.LogLevel) gormlogger.Interface {
	return l
}

// Info 委托打印 info 级日志（仅开启调试时实际输出）。
func (l *Logger) Info(ctx context.Context, msg string, data ...interface{}) {
	l.active().Info(ctx, msg, data...)
}

// Warn 委托打印 warn 级日志。
func (l *Logger) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.active().Warn(ctx, msg, data...)
}

// Error 委托打印 error 级日志。
func (l *Logger) Error(ctx context.Context, msg string, data ...interface{}) {
	l.active().Error(ctx, msg, data...)
}

// Trace 委托打印 SQL 执行轨迹（仅开启调试时实际输出普通 SQL）。
func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.active().Trace(ctx, begin, fc, err)
}
