package migration

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestIsHighRiskSettingKeyName(t *testing.T) {
	for _, tt := range []struct {
		key  string
		want bool
	}{
		{key: "database_path", want: true},
		{key: "databasePath", want: true},
		{key: "sqlite-db-path", want: true},
		{key: "listen_port", want: true},
		{key: "HTTPListenPort", want: true},
		{key: "jwt_signing_algorithm", want: true},
		{key: "JWTSecret", want: true},
		{key: "api_secret", want: true},
		{key: "client_secret", want: true},
		{key: "admin_password", want: true},
		{key: "access_token", want: true},
		{key: "masterPassword", want: true},
		{key: "smb_credential", want: true},
		{key: "api_key", want: true},
		{key: "access_key", want: true},
		{key: "private_key", want: true},
		{key: "database_url", want: true},
		{key: "database_dsn", want: true},
		{key: "sqlite_dsn", want: true},
		{key: "legacy_theme", want: false},
		{key: "scan_interval", want: false},
		{key: "portability_mode", want: false},
		{key: "tokenizer_model", want: false},
		{key: "keyboard_layout", want: false},
		{key: "public_key", want: false},
		{key: "database_pool_size", want: false},
	} {
		if got := isHighRiskSettingKeyName(tt.key); got != tt.want {
			t.Fatalf("isHighRiskSettingKeyName(%q)=%v，期望 %v", tt.key, got, tt.want)
		}
	}
}

func TestEstimateSettingsPreflightReportsWarningsAndBlockersWithoutWriting(t *testing.T) {
	db := openSettingsPreflightDB(t)
	rows := []models.Setting{
		{Key: "scan_interval", Value: "300"},
		{Key: "legacy_theme", Value: "dark"},
		{Key: "legacy_master_password", Value: "不得出现在诊断中"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入旧 settings fixture 失败: %v", err)
	}
	var before []models.Setting
	if err := db.Order("key").Find(&before).Error; err != nil {
		t.Fatalf("读取预检前 settings 失败: %v", err)
	}

	plan, err := estimateSettingsPreflight(context.Background(), db)
	if err != nil {
		t.Fatalf("settings 预检估算失败: %v", err)
	}
	if plan.EstimatedRows != 3 {
		t.Fatalf("预检行数不正确: got=%d want=3", plan.EstimatedRows)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "legacy_theme") {
		t.Fatalf("普通未知 key 应生成 warning: %v", plan.Warnings)
	}
	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], "legacy_master_password") {
		t.Fatalf("未知高风险 key 应生成 blocker: %v", plan.Blockers)
	}
	diagnostics := strings.Join(plan.Warnings, " ") + strings.Join(plan.Blockers, " ")
	if strings.Contains(diagnostics, "不得出现在诊断中") {
		t.Fatal("预检诊断不得泄露 settings 值")
	}
	if err := migrateSettingsPreflight(context.Background(), db); err != nil {
		t.Fatalf("settings 兼容迁移空操作失败: %v", err)
	}

	var got []models.Setting
	if err := db.Order("key").Find(&got).Error; err != nil {
		t.Fatalf("读取预检后 settings 失败: %v", err)
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("预检不得修改或删除 settings: before=%+v after=%+v", before, got)
	}
}

func TestEstimateSettingsPreflightBlocksInvalidRegisteredValue(t *testing.T) {
	db := openSettingsPreflightDB(t)
	if err := db.Create(&models.Setting{Key: "scan_interval", Value: "invalid"}).Error; err != nil {
		t.Fatalf("写入非法已知设置失败: %v", err)
	}

	plan, err := estimateSettingsPreflight(context.Background(), db)
	if err != nil {
		t.Fatalf("settings 预检估算失败: %v", err)
	}
	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], "scan_interval") {
		t.Fatalf("已知 key 的 registry 校验失败应生成 blocker: %v", plan.Blockers)
	}
}

func TestSettingsPreflightDryRunAggregatesWarningAndBlocker(t *testing.T) {
	db := openSettingsPreflightDB(t)
	rows := []models.Setting{
		{Key: "legacy_theme", Value: "dark"},
		{Key: "legacy_access_token", Value: "secret"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入 dry-run settings fixture 失败: %v", err)
	}
	registry, err := NewRegistry(Migration{
		ID:          "20260712_0015_fr2_017_settings_preflight",
		Description: "预检旧 settings",
		Estimate:    estimateSettingsPreflight,
	})
	if err != nil {
		t.Fatalf("创建 settings 预检 registry 失败: %v", err)
	}

	plan, err := NewRunner(db, RunnerOptions{Registry: registry}).DryRun(context.Background())
	if err != nil {
		t.Fatalf("settings 预检 dry-run 失败: %v", err)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "legacy_theme") {
		t.Fatalf("dry-run 应聚合普通未知 key warning: %v", plan.Warnings)
	}
	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], "legacy_access_token") {
		t.Fatalf("dry-run 应聚合未知高风险 key blocker: %v", plan.Blockers)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].EstimatedRows != 2 || len(plan.Steps[0].Warnings) != 1 {
		t.Fatalf("dry-run 单步预检信息不完整: %+v", plan.Steps)
	}
}

func TestValidateSettingsPreflightAllowsWarningButRejectsBlocker(t *testing.T) {
	db := openSettingsPreflightDB(t)
	if err := db.Create(&models.Setting{Key: "legacy_theme", Value: "dark"}).Error; err != nil {
		t.Fatalf("写入普通未知设置失败: %v", err)
	}
	validation, err := validateSettingsPreflight(context.Background(), db)
	if err != nil {
		t.Fatalf("普通未知设置不应阻断迁移: %v", err)
	}
	if !strings.Contains(validation.Summary, "1 个警告") {
		t.Fatalf("校验摘要应包含 warning 数量: %q", validation.Summary)
	}

	if err := db.Create(&models.Setting{Key: "legacy_access_token", Value: "secret"}).Error; err != nil {
		t.Fatalf("写入高风险未知设置失败: %v", err)
	}
	if _, err := validateSettingsPreflight(context.Background(), db); err == nil || !strings.Contains(err.Error(), "legacy_access_token") {
		t.Fatalf("未知高风险设置应阻断迁移: %v", err)
	}
}

func TestDefaultMigrationsAppendSettingsPreflight(t *testing.T) {
	migrations := DefaultMigrations()
	last := migrations[len(migrations)-1]
	if last.ID != "20260712_0015_fr2_017_settings_preflight" {
		t.Fatalf("settings 预检 migration 应使用新递增 ID: %s", last.ID)
	}
	if last.Estimate == nil || last.Up == nil || last.Validate == nil || !last.SafeToRetry {
		t.Fatalf("settings 预检 migration 契约不完整: %+v", last)
	}
}

func openSettingsPreflightDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 settings 预检测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("创建 settings 表失败: %v", err)
	}
	return db
}
