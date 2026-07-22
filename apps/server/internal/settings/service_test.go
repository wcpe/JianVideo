package settings

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// setupTestDB 创建测试用的内存数据库并迁移 settings 表。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移 settings 表失败: %v", err)
	}
	return db
}

func TestParseBoolSettingAcceptsRegistryBooleanForms(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{"1", true}, {"0", false}, {"true", true}, {"TRUE", true},
		{"false", false}, {"FALSE", false}, {" True ", true}, {" False ", false},
	} {
		if got := ParseBoolSetting(tt.raw, !tt.want); got != tt.want {
			t.Fatalf("ParseBoolSetting(%q)=%v，期望 %v", tt.raw, got, tt.want)
		}
	}
}

func TestSetAndGet(t *testing.T) {
	svc := NewService(setupTestDB(t))

	if err := svc.Set(KeyScanInterval, "3600"); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}

	v, err := svc.Get(KeyScanInterval)
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if v != "3600" {
		t.Fatalf("期望值 3600, 实际 %q", v)
	}
}

func TestGetMissingReturnsEmpty(t *testing.T) {
	svc := NewService(setupTestDB(t))

	v, err := svc.Get("not_exist")
	if err != nil {
		t.Fatalf("读取缺失键不应报错, 实际: %v", err)
	}
	if v != "" {
		t.Fatalf("缺失键应返回空串, 实际 %q", v)
	}
}

// TestSetUpsert 同一 key 重复写入应覆盖旧值，不报主键冲突。
func TestSetUpsert(t *testing.T) {
	svc := NewService(setupTestDB(t))

	if err := svc.Set(KeyScanInterval, "60"); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := svc.Set(KeyScanInterval, "120"); err != nil {
		t.Fatalf("覆盖写入失败（疑似主键冲突）: %v", err)
	}

	v, _ := svc.Get(KeyScanInterval)
	if v != "120" {
		t.Fatalf("期望覆盖为 120, 实际 %q", v)
	}
}

// TestScanInterval 周期解析（FR-28）：有效正整数转 Duration；空 / 非法 / <=0 视为关闭返回 0。
func TestScanInterval(t *testing.T) {
	svc := NewService(setupTestDB(t))

	// 未设置 → 0
	if d := svc.ScanInterval(); d != 0 {
		t.Fatalf("未设置时应为 0, 实际 %v", d)
	}

	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"3600", 3600 * time.Second},
		{" 600 ", 600 * time.Second}, // 含空白
		{"0", 0},                     // 0 关闭
		{"-5", 0},                    // 负数关闭
		{"abc", 0},                   // 非法关闭
		{"", 0},                      // 空关闭
	}
	for _, c := range cases {
		if err := svc.repo.Upsert(context.Background(), KeyScanInterval, c.raw); err != nil {
			t.Fatalf("写入 %q 失败: %v", c.raw, err)
		}
		if got := svc.ScanInterval(); got != c.want {
			t.Fatalf("raw=%q 期望 %v, 实际 %v", c.raw, c.want, got)
		}
	}
}

func TestGetAll(t *testing.T) {
	svc := NewService(setupTestDB(t))
	_ = svc.Set(KeyScanInterval, "300")
	_ = svc.Set(KeyRecycleBinPaths, `{"D":"D:/.recycle"}`)

	all, err := svc.GetAll()
	if err != nil {
		t.Fatalf("读取全部设置失败: %v", err)
	}
	if all[KeyScanInterval] != "300" || all[KeyRecycleBinPaths] != `{"D":"D:/.recycle"}` {
		t.Fatalf("GetAll 返回不正确: %v", all)
	}
	if all[KeyMediaInferenceEnabled] != "1" {
		t.Fatalf("影视推断默认应开启, 实际 %q", all[KeyMediaInferenceEnabled])
	}
	if all[KeyMediaInferenceDisabledLibraries] != "[]" {
		t.Fatalf("每库影视推断关闭列表默认应为空数组, 实际 %q", all[KeyMediaInferenceDisabledLibraries])
	}
}

func TestSetManyAtomicAndPersist(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)

	err := svc.SetMany(map[string]string{
		KeyScanInterval:    "900",
		KeyRecycleBinPaths: `{"E":"E:/trash"}`,
	})
	if err != nil {
		t.Fatalf("批量写入失败: %v", err)
	}

	// 用同一底层数据库新建服务实例，验证持久化（非内存态残留）
	svc2 := NewService(db)
	all, err := svc2.GetAll()
	if err != nil {
		t.Fatalf("二次读取失败: %v", err)
	}
	if all[KeyScanInterval] != "900" {
		t.Fatalf("扫描周期未持久化, 实际 %q", all[KeyScanInterval])
	}
	if all[KeyRecycleBinPaths] != `{"E":"E:/trash"}` {
		t.Fatalf("回收站路径未持久化, 实际 %q", all[KeyRecycleBinPaths])
	}
}

func TestSetManyWithHookRetriesRepeatedWALSnapshotConflicts(t *testing.T) {
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "settings-snapshot.db")) + "?_busy_timeout=1000&_journal_mode=WAL"
	dbA := openSettingsWALDB(t, dsn)
	dbB := openSettingsWALDB(t, dsn)
	if err := dbA.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移设置表失败: %v", err)
	}
	if err := dbA.Create(&models.Setting{Key: KeyScanInterval, Value: "100", UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("写入初始设置失败: %v", err)
	}

	const (
		callbackName     = "test:settings-busy-snapshot"
		conflictAttempts = 4
	)
	var attempts atomic.Int32
	var injected atomic.Int32
	if err := dbA.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "settings" {
			return
		}
		attempt := attempts.Add(1)
		if attempt > conflictAttempts {
			return
		}
		injected.Add(1)
		value := fmt.Sprintf("10%d", attempt)
		if err := dbB.Model(&models.Setting{}).Where("key = ?", KeyScanInterval).Updates(map[string]any{
			"value":      value,
			"updated_at": time.Now(),
		}).Error; err != nil {
			t.Errorf("注入 WAL 并发写失败: %v", err)
		}
	}); err != nil {
		t.Fatalf("注册 WAL 快照冲突回调失败: %v", err)
	}
	t.Cleanup(func() { _ = dbA.Callback().Query().Remove(callbackName) })

	var hookCalls atomic.Int32
	var hookBefore string
	svc := NewService(dbA)
	err := svc.SetManyWithHook(context.Background(), map[string]string{KeyScanInterval: "900"}, func(_ context.Context, _ TxRepository, before, _ map[string]string) error {
		hookCalls.Add(1)
		hookBefore = before[KeyScanInterval]
		return nil
	})
	if err != nil {
		t.Fatalf("连续 WAL 快照冲突后设置事务应自动重试: %v", err)
	}
	if attempts.Load() != conflictAttempts+1 || injected.Load() != conflictAttempts {
		t.Fatalf("连续 WAL 快照冲突应在第 5 次事务成功：事务尝试=%d 冲突注入=%d", attempts.Load(), injected.Load())
	}
	if hookCalls.Load() != 1 || hookBefore != "104" {
		t.Fatalf("成功重试应使用最新事务快照执行事务钩子：调用次数=%d 前值=%q", hookCalls.Load(), hookBefore)
	}
	var setting models.Setting
	if err := dbB.First(&setting, "key = ?", KeyScanInterval).Error; err != nil {
		t.Fatalf("读取最终设置失败: %v", err)
	}
	if setting.Value != "900" {
		t.Fatalf("目标设置最终值应为 900, 实际 %q", setting.Value)
	}
}

func openSettingsWALDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开设置 WAL 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取设置 WAL 底层数据库失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestSetManyRejectsUnknownKeyAtomically(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)

	err := svc.SetMany(map[string]string{
		KeyScanInterval: "900",
		"typo_key":      "bad",
	})
	if err == nil || !IsValidationError(err) {
		t.Fatalf("未知 key 应返回校验错误, 实际 %v", err)
	}

	all, err := svc.GetAll()
	if err != nil {
		t.Fatalf("读取全部设置失败: %v", err)
	}
	if all[KeyScanInterval] == "900" {
		t.Fatalf("批量校验失败时不应写入任何设置")
	}
	var count int64
	if err := db.Model(&models.Setting{}).Where("key = ?", "typo_key").Count(&count).Error; err != nil {
		t.Fatalf("查询脏 key 失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("未知 key 不应落库, count=%d", count)
	}
}

func TestSetManyRejectsInvalidType(t *testing.T) {
	svc := NewService(setupTestDB(t))

	if err := svc.SetMany(map[string]string{KeyScanInterval: "not-int"}); err == nil || !IsValidationError(err) {
		t.Fatalf("非法扫描周期应返回校验错误, 实际 %v", err)
	}
}

func TestDefinitionsExposeRegisteredRuntimeKeys(t *testing.T) {
	defs := Definitions()
	seen := map[string]Definition{}
	for _, def := range defs {
		seen[def.Key] = def
	}
	for _, key := range []string{KeyScanInterval, KeyNetworkProxy, KeyOpenTabs, KeyLastOpenedPath, KeyTaskWorkerThumbnailConcurrency, KeyMediaInferenceEnabled, KeyMediaInferenceDisabledLibraries, KeyTranscodeHWAccelMode, KeyTranscodeHWAccelFallback, KeyTranscodeABRLadder} {
		if _, ok := seen[key]; !ok {
			t.Fatalf("definitions 缺少 key=%s", key)
		}
	}
	if def := seen[KeyNetworkProxy]; !def.Sensitive || def.Layer != LayerRuntime {
		t.Fatalf("network_proxy 应登记为运行期敏感项: %+v", def)
	}
	if def := seen["jwt_secret"]; !def.Sensitive || def.Layer != LayerStartup {
		t.Fatalf("jwt_secret 应登记为启动期敏感项: %+v", def)
	}
	if def := seen[KeyTaskWorkerThumbnailConcurrency]; def.DefaultValue != "4" || def.Consumer != "tasks.worker" {
		t.Fatalf("缩略图 worker 并发配置异常: %+v", def)
	}
	if def := seen[KeyTranscodeHWAccelMode]; def.DefaultValue != "auto" || def.ValueType != ValueEnum || def.Consumer != "transcoder" {
		t.Fatalf("硬件加速策略配置异常: %+v", def)
	}
	if def := seen[KeyTranscodeHWAccelFallback]; def.DefaultValue != "1" || def.ValueType != ValueBool || def.Consumer != "transcoder" {
		t.Fatalf("硬件加速回退配置异常: %+v", def)
	}
	if def := seen[KeyTranscodeABRLadder]; def.DefaultValue != `["1080p","720p","480p"]` || def.ValueType != ValueJSON || def.Consumer != "transcoder.abr" {
		t.Fatalf("ABR ladder 配置异常: %+v", def)
	}
}

func TestTranscodeABRLadderPersistsAndRejectsUnknownVariant(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if err := svc.Set(KeyTranscodeABRLadder, `["720p","480p"]`); err != nil {
		t.Fatalf("写入 ABR ladder 失败: %v", err)
	}
	if got := svc.TranscodeABRLadder(); len(got) != 2 || got[0] != "720p" || got[1] != "480p" {
		t.Fatalf("读取 ABR ladder 异常: %v", got)
	}
	if err := svc.Set(KeyTranscodeABRLadder, `["720p","未知档位"]`); err == nil || !IsValidationError(err) {
		t.Fatalf("未知 ABR 档位应被拒绝, 实际 %v", err)
	}
}

func TestTaskWorkerConcurrencySettings(t *testing.T) {
	svc := NewService(setupTestDB(t))

	if err := svc.Set(KeyTaskWorkerThumbnailConcurrency, "4"); err != nil {
		t.Fatalf("写入合法任务并发配置失败: %v", err)
	}
	if err := svc.Set(KeyTaskWorkerThumbnailConcurrency, "0"); err == nil || !IsValidationError(err) {
		t.Fatalf("任务并发配置必须拒绝非正整数, 实际 %v", err)
	}
}

func TestHWAccelSettingsPersistReloadAndValidate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)

	if err := svc.SetMany(map[string]string{
		KeyTranscodeHWAccelMode:     "qsv",
		KeyTranscodeHWAccelFallback: "0",
	}); err != nil {
		t.Fatalf("写入硬件加速策略失败: %v", err)
	}

	reloaded := NewService(db)
	all, err := reloaded.GetAll()
	if err != nil {
		t.Fatalf("重载设置失败: %v", err)
	}
	if all[KeyTranscodeHWAccelMode] != "qsv" || all[KeyTranscodeHWAccelFallback] != "0" {
		t.Fatalf("硬件加速策略未持久化，实际: %v", all)
	}

	if err := svc.Set(KeyTranscodeHWAccelMode, "vulkan"); err == nil || !IsValidationError(err) {
		t.Fatalf("用户策略不支持 vulkan，应返回校验错误，实际 %v", err)
	}
}

func TestGetAllRedactsSensitiveSettings(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if err := svc.Set(KeyNetworkProxy, "http://user:secret@example.com:8080"); err != nil {
		t.Fatalf("写入代理失败: %v", err)
	}

	all, err := svc.GetAll()
	if err != nil {
		t.Fatalf("读取全部设置失败: %v", err)
	}
	if all[KeyNetworkProxy] != sensitiveDisplayValue {
		t.Fatalf("敏感代理不应明文回显, 实际 %q", all[KeyNetworkProxy])
	}
	if raw, err := svc.Get(KeyNetworkProxy); err != nil || raw != "http://user:secret@example.com:8080" {
		t.Fatalf("内部读取应保留原始值, raw=%q err=%v", raw, err)
	}
}

// TestDebugLog 调试日志开关读取（FR-110）：缺失=关、"1"/"true"=开、其余=关。
func TestDebugLog(t *testing.T) {
	svc := NewService(setupTestDB(t))

	// 缺失时默认关闭
	if svc.DebugLog() {
		t.Fatalf("未设置时应为关闭")
	}

	// 写入 "1" 开启
	if err := svc.Set(KeyDebugLog, "1"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if !svc.DebugLog() {
		t.Fatalf("写入 1 后应为开启")
	}

	// 写入 "0" 关闭
	if err := svc.Set(KeyDebugLog, "0"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if svc.DebugLog() {
		t.Fatalf("写入 0 后应为关闭")
	}
}

// TestParseDebugLog 纯函数解析各取值。
func TestParseDebugLog(t *testing.T) {
	const paddedTrueSetting = " 1 "

	cases := map[string]bool{
		"1": true, "true": true, paddedTrueSetting: true,
		"0": false, "false": false, "": false, "yes": false,
	}
	for in, want := range cases {
		if got := ParseDebugLog(in); got != want {
			t.Errorf("ParseDebugLog(%q)=%v, 期望 %v", in, got, want)
		}
	}
}
