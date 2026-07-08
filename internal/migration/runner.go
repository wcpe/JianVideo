package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// 注册 sqlite3 driver，备份完整性校验需要通过 database/sql 打开副本。
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

// schema migration 状态常量。
const (
	MigrationStatusPending   = "pending"
	MigrationStatusRunning   = "running"
	MigrationStatusSucceeded = "succeeded"
	MigrationStatusFailed    = "failed"
)

// RunnerOptions 定义迁移执行器的外部依赖。
type RunnerOptions struct {
	DBPath    string
	BackupDir string
	Registry  *Registry
	Now       func() time.Time
}

// Runner 执行版本化 schema 迁移。
type Runner struct {
	db      *gorm.DB
	options RunnerOptions
}

// StepPlan 表示 dry-run 输出中的单步计划。
type StepPlan struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	EstimatedRows  int64  `json:"estimated_rows"`
	WillRun        bool   `json:"will_run"`
	AlreadyApplied bool   `json:"already_applied"`
}

// Plan 表示 dry-run 只读计划。
type Plan struct {
	Steps    []StepPlan `json:"steps"`
	Blockers []string   `json:"blockers"`
}

// BackupResult 表示迁移前 SQLite 备份结果。
type BackupResult struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	IntegrityOK bool   `json:"integrity_ok"`
}

// RunResult 表示真实迁移执行结果。
type RunResult struct {
	Backup  BackupResult `json:"backup"`
	Applied []string     `json:"applied"`
	Skipped []string     `json:"skipped"`
}

type migrationRow struct {
	ID                string
	Status            string
	ValidationSummary string
}

// NewRunner 创建迁移执行器。
func NewRunner(db *gorm.DB, options RunnerOptions) *Runner {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.BackupDir == "" && options.DBPath != "" {
		options.BackupDir = filepath.Join(filepath.Dir(options.DBPath), "backups")
	}
	return &Runner{db: db, options: options}
}

// DryRun 只读返回迁移计划，不写业务表、schema_migrations 或审计表。
func (r *Runner) DryRun(ctx context.Context) (Plan, error) {
	var plan Plan
	for _, migration := range r.options.Registry.all() {
		row, exists, err := r.findMigration(migration.ID)
		if err != nil {
			return Plan{}, err
		}
		step := StepPlan{
			ID:             migration.ID,
			Description:    migration.Description,
			WillRun:        !exists || row.Status != MigrationStatusSucceeded,
			AlreadyApplied: exists && row.Status == MigrationStatusSucceeded,
		}
		if migration.Estimate != nil {
			estimated, err := migration.Estimate(ctx, r.db)
			if err != nil {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s: %v", migration.ID, err))
			} else {
				step.EstimatedRows = estimated.EstimatedRows
			}
		}
		plan.Steps = append(plan.Steps, step)
	}
	return plan, nil
}

// Run 先备份并校验 SQLite，再执行待迁移步骤。
func (r *Runner) Run(ctx context.Context) (RunResult, error) {
	var result RunResult
	pending, err := r.pendingMigrations(ctx)
	if err != nil {
		return result, err
	}
	if len(pending) == 0 {
		return result, nil
	}

	backup, err := r.createBackup()
	if err != nil {
		return result, err
	}
	result.Backup = backup

	if err := r.ensureMetadataTables(); err != nil {
		return result, err
	}
	if err := r.recordAudit("migration.started", "", "数据库迁移开始", backup); err != nil {
		return result, err
	}

	for _, migration := range pending {
		applied, err := r.runMigration(ctx, migration, backup)
		if err != nil {
			_ = r.recordAudit("migration.failed", migration.ID, err.Error(), backup)
			return result, err
		}
		if applied {
			result.Applied = append(result.Applied, migration.ID)
		} else {
			result.Skipped = append(result.Skipped, migration.ID)
		}
	}
	if err := r.recordAudit("migration.succeeded", "", "数据库迁移完成", backup); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Runner) pendingMigrations(ctx context.Context) ([]Migration, error) {
	var pending []Migration
	for _, migration := range r.options.Registry.all() {
		row, exists, err := r.findMigration(migration.ID)
		if err != nil {
			return nil, err
		}
		if exists && row.Status == MigrationStatusSucceeded {
			if migration.Validate != nil {
				if _, err := migration.Validate(ctx, r.db); err != nil {
					if !migration.SafeToRetry {
						return nil, fmt.Errorf("migration %s 已成功但校验失败且不可重试: %w", migration.ID, err)
					}
					pending = append(pending, migration)
				}
			}
			continue
		}
		if exists && row.Status == MigrationStatusFailed && !migration.SafeToRetry {
			return nil, fmt.Errorf("migration %s 上次失败且不可安全重试", migration.ID)
		}
		pending = append(pending, migration)
	}
	return pending, nil
}

func (r *Runner) runMigration(ctx context.Context, migration Migration, backup BackupResult) (bool, error) {
	if err := r.markRunning(migration, backup.Path); err != nil {
		return false, err
	}
	if migration.Up != nil {
		if err := r.db.Transaction(func(tx *gorm.DB) error {
			return migration.Up(ctx, tx)
		}); err != nil {
			r.markFailed(migration.ID, err)
			return false, fmt.Errorf("migration %s 执行失败: %w", migration.ID, err)
		}
	}
	validationSummary := ""
	if migration.Validate != nil {
		validation, err := migration.Validate(ctx, r.db)
		if err != nil {
			r.markFailed(migration.ID, err)
			return false, fmt.Errorf("migration %s 校验失败: %w", migration.ID, err)
		}
		validationSummary = validation.Summary
	}
	if err := r.markSucceeded(migration.ID, validationSummary); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) createBackup() (BackupResult, error) {
	if r.options.DBPath == "" || strings.Contains(r.options.DBPath, ":memory:") {
		return BackupResult{}, fmt.Errorf("真实迁移需要文件型 SQLite 数据库路径")
	}
	if err := os.MkdirAll(r.options.BackupDir, 0o750); err != nil {
		return BackupResult{}, fmt.Errorf("创建备份目录失败: %w", err)
	}
	name := fmt.Sprintf("%s-before-v2-%s.sqlite",
		strings.TrimSuffix(filepath.Base(r.options.DBPath), filepath.Ext(r.options.DBPath)),
		r.options.Now().UTC().Format("20060102T150405Z"),
	)
	backupPath := uniqueBackupPath(r.options.BackupDir, name)
	if err := r.db.Exec("VACUUM INTO ?", backupPath).Error; err != nil {
		return BackupResult{}, fmt.Errorf("创建 SQLite 备份失败: %w", err)
	}
	stat, err := os.Stat(backupPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("读取备份文件失败: %w", err)
	}
	if got, err := integrityCheck(backupPath); err != nil {
		return BackupResult{}, err
	} else if got != "ok" {
		return BackupResult{}, fmt.Errorf("备份 integrity_check 失败: %s", got)
	}
	return BackupResult{Path: backupPath, Size: stat.Size(), IntegrityOK: true}, nil
}

func uniqueBackupPath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func integrityCheck(dbPath string) (string, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return "", fmt.Errorf("打开备份库失败: %w", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()
	var result string
	if err := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return "", fmt.Errorf("备份 integrity_check 执行失败: %w", err)
	}
	return result, nil
}

func (r *Runner) ensureMetadataTables() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			id TEXT PRIMARY KEY,
			description TEXT,
			status TEXT NOT NULL,
			safe_to_retry INTEGER NOT NULL DEFAULT 1,
			started_at DATETIME,
			completed_at DATETIME,
			error_summary TEXT,
			validation_summary TEXT,
			backup_path TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope TEXT NOT NULL,
			space_id TEXT,
			event_type TEXT NOT NULL,
			migration_id TEXT,
			message TEXT,
			metadata_json TEXT,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_scope_created ON audit_events(scope, created_at);`,
	}
	for _, stmt := range statements {
		if err := r.db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("初始化迁移元数据表失败: %w", err)
		}
	}
	return nil
}

func (r *Runner) findMigration(id string) (migrationRow, bool, error) {
	if !tableExists(r.db, "schema_migrations") {
		return migrationRow{}, false, nil
	}
	var row migrationRow
	err := r.db.Raw("SELECT id, status, validation_summary FROM schema_migrations WHERE id = ?", id).Scan(&row).Error
	if err != nil {
		return migrationRow{}, false, err
	}
	if row.ID == "" {
		return migrationRow{}, false, nil
	}
	return row, true, nil
}

func (r *Runner) markRunning(migration Migration, backupPath string) error {
	now := r.options.Now()
	return r.db.Exec(
		`INSERT INTO schema_migrations
			(id, description, status, safe_to_retry, started_at, completed_at, error_summary, validation_summary, backup_path, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, NULL, '', '', ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			description = excluded.description,
			status = excluded.status,
			safe_to_retry = excluded.safe_to_retry,
			started_at = excluded.started_at,
			completed_at = NULL,
			error_summary = '',
			backup_path = excluded.backup_path,
			updated_at = excluded.updated_at`,
		migration.ID, migration.Description, MigrationStatusRunning, boolAsInt(migration.SafeToRetry),
		now, backupPath, now, now,
	).Error
}

func (r *Runner) markFailed(id string, cause error) {
	now := r.options.Now()
	r.db.Exec(
		`UPDATE schema_migrations
		 SET status = ?, completed_at = ?, error_summary = ?, updated_at = ?
		 WHERE id = ?`,
		MigrationStatusFailed, now, truncate(cause.Error(), 500), now, id,
	)
}

func (r *Runner) markSucceeded(id, validationSummary string) error {
	now := r.options.Now()
	return r.db.Exec(
		`UPDATE schema_migrations
		 SET status = ?, completed_at = ?, validation_summary = ?, updated_at = ?
		 WHERE id = ?`,
		MigrationStatusSucceeded, now, validationSummary, now, id,
	).Error
}

func (r *Runner) recordAudit(eventType, migrationID, message string, backup BackupResult) error {
	metadata := map[string]any{
		"backup_path":         backup.Path,
		"backup_size":         backup.Size,
		"backup_integrity_ok": backup.IntegrityOK,
	}
	raw, _ := json.Marshal(metadata)
	if err := r.db.Exec(
		`INSERT INTO audit_events(scope, space_id, event_type, migration_id, message, metadata_json, created_at)
		 VALUES ('system', NULL, ?, ?, ?, ?, ?)`,
		eventType, migrationID, message, string(raw), r.options.Now(),
	).Error; err != nil {
		return fmt.Errorf("写入迁移审计事件失败: %w", err)
	}
	return nil
}

func tableExists(db *gorm.DB, table string) bool {
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).
		Scan(&count).Error; err != nil {
		return false
	}
	return count == 1
}

func boolAsInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
