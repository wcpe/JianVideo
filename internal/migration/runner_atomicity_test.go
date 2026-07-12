package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunBlocksNonRetryableRunningMigration(t *testing.T) {
	gdb, dbPath := openRunnerAtomicityDB(t)
	migration := Migration{
		ID:          "20260708_9101_running_blocked",
		Description: "阻断不可重试的运行中迁移",
		SafeToRetry: false,
		Up: func(_ context.Context, _ *gorm.DB) error {
			t.Fatal("不可重试的运行中迁移不应再次执行")
			return nil
		},
	}
	runner := newRunnerForMigrations(t, gdb, dbPath, migration)
	seedMigrationStatus(t, runner, migration, MigrationStatusRunning)

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("不可重试的运行中迁移应阻断执行")
	}
	if !strings.Contains(err.Error(), "运行中且不可安全重试") {
		t.Fatalf("错误应说明运行中迁移不可重试: %v", err)
	}
	if got := migrationStatus(t, gdb, migration.ID); got != MigrationStatusRunning {
		t.Fatalf("被阻断的迁移状态不应变化: %s", got)
	}
}

func TestRunRetriesRetryableRunningMigration(t *testing.T) {
	gdb, dbPath := openRunnerAtomicityDB(t)
	upCalls := 0
	migration := Migration{
		ID:          "20260708_9102_running_retryable",
		Description: "重试可安全重试的运行中迁移",
		SafeToRetry: true,
		Up: func(_ context.Context, tx *gorm.DB) error {
			upCalls++
			return tx.Exec("CREATE TABLE running_retry_probe (id INTEGER PRIMARY KEY)").Error
		},
		Validate: func(_ context.Context, tx *gorm.DB) (Validation, error) {
			if !tableExists(tx, "running_retry_probe") {
				return Validation{}, errors.New("重试迁移未在事务内生效")
			}
			return Validation{Summary: "运行中迁移重试成功"}, nil
		},
	}
	runner := newRunnerForMigrations(t, gdb, dbPath, migration)
	seedMigrationStatus(t, runner, migration, MigrationStatusRunning)

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("可重试的运行中迁移应继续执行: %v", err)
	}
	if upCalls != 1 {
		t.Fatalf("可重试的运行中迁移应执行一次: %d", upCalls)
	}
	if got := migrationStatus(t, gdb, migration.ID); got != MigrationStatusSucceeded {
		t.Fatalf("重试成功后状态应为 succeeded: %s", got)
	}
}

func TestRunRollsBackUpAndValidateWhenMarkSucceededFails(t *testing.T) {
	gdb, dbPath := openRunnerAtomicityDB(t)
	migration := Migration{
		ID:          "20260708_9103_atomic_success",
		Description: "验证迁移成功提交原子性",
		SafeToRetry: true,
		Up: func(_ context.Context, tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE atomicity_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
				return err
			}
			return tx.Exec("INSERT INTO atomicity_probe(id) VALUES (1)").Error
		},
		Validate: func(_ context.Context, tx *gorm.DB) (Validation, error) {
			var count int64
			if err := tx.Table("atomicity_probe").Count(&count).Error; err != nil {
				return Validation{}, err
			}
			if count != 1 {
				return Validation{}, errors.New("事务内校验未看到迁移写入")
			}
			return Validation{Summary: "事务内校验通过"}, nil
		},
	}
	runner := newRunnerForMigrations(t, gdb, dbPath, migration)
	if err := runner.ensureMetadataTables(); err != nil {
		t.Fatalf("初始化迁移元数据表失败: %v", err)
	}
	if err := gdb.Exec(`
		CREATE TRIGGER reject_succeeded_status
		BEFORE UPDATE OF status ON schema_migrations
		WHEN NEW.status = 'succeeded'
		BEGIN
			SELECT RAISE(ABORT, '成功状态写入失败');
		END;
	`).Error; err != nil {
		t.Fatalf("创建成功状态失败触发器失败: %v", err)
	}

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("成功状态写入失败时迁移应失败")
	}
	if !strings.Contains(err.Error(), "成功状态写入失败") {
		t.Fatalf("错误应保留成功状态写入失败原因: %v", err)
	}
	if testTableExists(t, gdb, "atomicity_probe") {
		t.Fatal("markSucceeded 失败时 Up 写入必须随事务回滚")
	}
	if got := migrationStatus(t, gdb, migration.ID); got != MigrationStatusFailed {
		t.Fatalf("事务失败后应可靠记录 failed: %s", got)
	}
}

func TestRunCombinesMigrationMarkFailedAndFailureAuditErrors(t *testing.T) {
	gdb, dbPath := openRunnerAtomicityDB(t)
	migration := Migration{
		ID:          "20260708_9104_combined_errors",
		Description: "验证失败错误合并",
		SafeToRetry: true,
		Up: func(_ context.Context, _ *gorm.DB) error {
			return errors.New("原始迁移失败")
		},
	}
	runner := newRunnerForMigrations(t, gdb, dbPath, migration)
	if err := runner.ensureMetadataTables(); err != nil {
		t.Fatalf("初始化迁移元数据表失败: %v", err)
	}
	if err := gdb.Exec(`
		CREATE TRIGGER reject_failed_status
		BEFORE UPDATE OF status ON schema_migrations
		WHEN NEW.status = 'failed'
		BEGIN
			SELECT RAISE(ABORT, '失败状态写入失败');
		END;
	`).Error; err != nil {
		t.Fatalf("创建失败状态触发器失败: %v", err)
	}
	if err := gdb.Exec(`
		CREATE TRIGGER reject_failure_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'migration.failed'
		BEGIN
			SELECT RAISE(ABORT, '失败审计写入失败');
		END;
	`).Error; err != nil {
		t.Fatalf("创建失败审计触发器失败: %v", err)
	}

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("迁移失败时应返回错误")
	}
	for _, part := range []string{"原始迁移失败", "失败状态写入失败", "失败审计写入失败"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("合并错误缺少 %q: %v", part, err)
		}
	}
}

func TestRunStopsOnPreflightBlockersBeforeBackupOrWrites(t *testing.T) {
	gdb, dbPath := openRunnerAtomicityDB(t)
	if err := gdb.Exec("CREATE TABLE preflight_sentinel (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("创建预检哨兵表失败: %v", err)
	}
	if err := gdb.Exec("INSERT INTO preflight_sentinel(id) VALUES (1)").Error; err != nil {
		t.Fatalf("写入预检哨兵失败: %v", err)
	}
	migration := Migration{
		ID:          "20260708_9105_preflight_blocker",
		Description: "验证预检阻断",
		SafeToRetry: true,
		Estimate: func(_ context.Context, _ *gorm.DB) (StepPlan, error) {
			return StepPlan{}, errors.New("发现高风险阻断项")
		},
		Up: func(_ context.Context, _ *gorm.DB) error {
			t.Fatal("预检存在 blocker 时不应执行 Up")
			return nil
		},
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	runner := newRunnerWithBackupDir(t, gdb, dbPath, backupDir, migration)

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("预检存在 blocker 时 Run 应失败")
	}
	if !strings.Contains(err.Error(), "发现高风险阻断项") {
		t.Fatalf("预检错误应包含 blocker: %v", err)
	}
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Fatalf("预检阻断时不应创建备份目录: %v", statErr)
	}
	if testTableExists(t, gdb, "schema_migrations") {
		t.Fatal("预检阻断时不应创建 schema_migrations")
	}
	if testTableExists(t, gdb, "audit_events") {
		t.Fatal("预检阻断时不应创建 audit_events")
	}
	if got := countRows(t, gdb, "preflight_sentinel"); got != 1 {
		t.Fatalf("预检阻断时不应修改业务表: %d", got)
	}
}

func TestRunAllowsPreflightWarnings(t *testing.T) {
	gdb, dbPath := openRunnerAtomicityDB(t)
	migration := Migration{
		ID:          "20260708_9106_preflight_warning",
		Description: "验证普通预检提醒",
		SafeToRetry: true,
		Estimate: func(_ context.Context, _ *gorm.DB) (StepPlan, error) {
			return StepPlan{EstimatedRows: 1, Warnings: []string{"预计迁移一行数据"}}, nil
		},
		Up: func(_ context.Context, tx *gorm.DB) error {
			return tx.Exec("CREATE TABLE preflight_warning_probe (id INTEGER PRIMARY KEY)").Error
		},
	}
	runner := newRunnerForMigrations(t, gdb, dbPath, migration)

	plan, err := runner.DryRun(context.Background())
	if err != nil {
		t.Fatalf("普通预检提醒不应导致 dry-run 失败: %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("普通预检提醒不应成为 blocker: %v", plan.Blockers)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "预计迁移一行数据") {
		t.Fatalf("dry-run 应返回普通预检提醒: %v", plan.Warnings)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("普通预检提醒不应阻断真实迁移: %v", err)
	}
	if !testTableExists(t, gdb, "preflight_warning_probe") {
		t.Fatal("存在普通预检提醒时仍应执行迁移")
	}
}

func openRunnerAtomicityDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "runner-atomicity.db")
	gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 Runner 测试库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return gdb, dbPath
}

func newRunnerForMigrations(t *testing.T, gdb *gorm.DB, dbPath string, migrations ...Migration) *Runner {
	t.Helper()
	return newRunnerWithBackupDir(t, gdb, dbPath, filepath.Join(t.TempDir(), "backups"), migrations...)
}

func newRunnerWithBackupDir(t *testing.T, gdb *gorm.DB, dbPath, backupDir string, migrations ...Migration) *Runner {
	t.Helper()
	registry, err := NewRegistry(migrations...)
	if err != nil {
		t.Fatalf("创建 Runner 测试 registry 失败: %v", err)
	}
	return NewRunner(gdb, RunnerOptions{
		DBPath:    dbPath,
		BackupDir: backupDir,
		Registry:  registry,
		Now:       fixedNow,
	})
}

func seedMigrationStatus(t *testing.T, runner *Runner, migration Migration, status string) {
	t.Helper()
	if err := runner.ensureMetadataTables(); err != nil {
		t.Fatalf("初始化迁移元数据表失败: %v", err)
	}
	now := fixedNow()
	if err := runner.db.Exec(`
		INSERT INTO schema_migrations(
			id, description, status, safe_to_retry, started_at, completed_at,
			error_summary, validation_summary, backup_path, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, NULL, '', '', '', ?, ?)
	`, migration.ID, migration.Description, status, boolAsInt(migration.SafeToRetry), now, now, now).Error; err != nil {
		t.Fatalf("写入迁移状态失败: %v", err)
	}
}
