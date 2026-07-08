//go:build ignore

// Package main 提供 FR2-037 通用任务队列 SQLite 查询基准命令。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	maxRows       int64 = 10_000_000
	pageSize      int   = 50
	sampleCount   int   = 32
	warmupCount   int   = 3
	schemaVersion int   = 3705
)

type querySpec struct {
	Args    []any  `json:"args"`
	Dataset string `json:"dataset"`
	Index   string `json:"index"`
	Query   string `json:"query"`
	Rows    int64  `json:"rows"`
	SQL     string `json:"sql"`
}

type queryResult struct {
	Args          []any    `json:"args"`
	DatabaseBytes int64    `json:"database_bytes"`
	Dataset       string   `json:"dataset"`
	Index         string   `json:"index"`
	IndexPlan     []string `json:"index_plan"`
	P95           float64  `json:"p95_ms"`
	P99           float64  `json:"p99_ms"`
	Pass          bool     `json:"pass"`
	Query         string   `json:"query"`
	Samples       int      `json:"samples"`
	ScannedRows   int64    `json:"scanned_rows"`
	SQL           string   `json:"sql"`
	ThresholdMS   float64  `json:"threshold_ms"`
}

type report struct {
	GeneratedAt string        `json:"generated_at"`
	Results     []queryResult `json:"results"`
}

func main() {
	outputDir := filepath.Join(".tmp", "benchmark", "fr2-037", time.Now().Format("20060102-150405"))
	if err := run(outputDir); err != nil {
		writeFailure(outputDir, err)
		exit(err)
	}
}

func run(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(".tmp", "benchmark", "fr2-037", "task-queue.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()
	db.SetMaxOpenConns(1)

	if err := configure(db); err != nil {
		return err
	}
	results, err := runBenchmarks(db, dbPath)
	if err != nil {
		return err
	}
	r := report{GeneratedAt: time.Now().Format(time.RFC3339), Results: results}
	if err := writeJSON(filepath.Join(outputDir, "results.json"), r); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "summary.md"), []byte(renderSummary(r)), 0o644); err != nil {
		return err
	}
	return assertBenchmarkPass(results)
}

func configure(db *sql.DB) error {
	statements := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = OFF",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -262144",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureDataset(db *sql.DB, targetRows int64) error {
	var userVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		return err
	}
	var count int64
	if userVersion == schemaVersion {
		if err := db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count); err == nil && count == targetRows {
			return nil
		}
	}
	if err := rebuildDataset(db, targetRows); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}

func rebuildDataset(db *sql.DB, targetRows int64) error {
	setupStatements := []string{
		"DROP TABLE IF EXISTS tasks",
		`CREATE TABLE tasks (
		id INTEGER PRIMARY KEY,
		scope TEXT NOT NULL,
		space_id TEXT,
		type TEXT NOT NULL,
		status TEXT NOT NULL,
		priority INTEGER NOT NULL,
		attempts INTEGER NOT NULL,
		max_attempts INTEGER NOT NULL,
		progress REAL NOT NULL,
		checkpoint TEXT,
		idempotency_key TEXT,
		payload_json TEXT,
		resource_type TEXT,
		resource_id TEXT,
		error TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		next_run_at INTEGER,
		started_at INTEGER,
		finished_at INTEGER
	)`,
	}
	for _, statement := range setupStatements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	insertStatement := `WITH RECURSIVE seq(id) AS (
		SELECT 1
		UNION ALL
		SELECT id + 1 FROM seq WHERE id < ?
	)
	INSERT INTO tasks
	SELECT
		id,
		CASE WHEN id % 100 = 0 THEN 'system' ELSE 'space' END,
		CASE WHEN id % 100 = 0 THEN NULL ELSE printf('space-%02d', (id % 10) + 1) END,
		CASE id % 5
			WHEN 0 THEN 'library.scan'
			WHEN 1 THEN 'transcode.hls'
			WHEN 2 THEN 'thumbnail.generate'
			WHEN 3 THEN 'metadata.backfill'
			ELSE 'cache.cleanup'
		END,
		CASE id % 5
			WHEN 0 THEN 'pending'
			WHEN 1 THEN 'running'
			WHEN 2 THEN 'succeeded'
			WHEN 3 THEN 'failed'
			ELSE 'canceled'
		END,
		id % 100,
		id % 3,
		3,
		(id % 100) / 100.0,
		'',
		printf('idem-%08d', id),
		'{}',
		CASE WHEN id % 2 = 0 THEN 'media' ELSE 'library' END,
		printf('%d', id % 100000),
		CASE WHEN id % 17 = 0 THEN '模拟错误' ELSE '' END,
		1893456000 - id,
		1893456000 - id + 30,
		CASE WHEN id % 97 = 0 THEN 1893456060 ELSE NULL END,
		CASE WHEN id % 5 = 1 THEN 1893456000 - id + 5 ELSE NULL END,
		CASE WHEN id % 5 IN (2, 3, 4) THEN 1893456000 - id + 60 ELSE NULL END
	FROM seq`
	if _, err := db.Exec(insertStatement, targetRows); err != nil {
		return err
	}
	indexStatements := []string{
		"CREATE INDEX idx_tasks_space_status_priority_created ON tasks(space_id, status, priority DESC, created_at, id)",
		"CREATE INDEX idx_tasks_type_status_priority_created ON tasks(type, status, priority DESC, created_at, id)",
		"CREATE INDEX idx_tasks_space_type_status_updated ON tasks(space_id, type, status, updated_at)",
		"CREATE INDEX idx_tasks_scope_space_type_status_id ON tasks(scope, space_id, type, status, id)",
		"CREATE INDEX idx_tasks_status_next_run_priority_created ON tasks(status, next_run_at, priority, created_at, id)",
		"CREATE UNIQUE INDEX idx_tasks_idempotency_active ON tasks(idempotency_key) WHERE idempotency_key <> '' AND status IN ('pending', 'running')",
		"ANALYZE",
	}
	for _, statement := range indexStatements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func runBenchmarks(db *sql.DB, dbPath string) ([]queryResult, error) {
	datasets := []struct {
		name string
		rows int64
	}{
		{name: "media-index-1m", rows: 1_000_000},
		{name: "media-index-5m", rows: 5_000_000},
		{name: "media-index-10m", rows: maxRows},
	}
	results := make([]queryResult, 0, len(datasets)*4)
	for _, dataset := range datasets {
		if err := ensureDataset(db, dataset.rows); err != nil {
			return nil, fmt.Errorf("%s 数据集准备失败: %w", dataset.name, err)
		}
		for _, spec := range buildSpecs(dataset.name, dataset.rows) {
			result, err := runQuery(db, dbPath, spec)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
	}
	return results, nil
}

func buildSpecs(dataset string, rows int64) []querySpec {
	return []querySpec{
		listSpec(dataset, rows),
		statsSpec(dataset, rows),
		claimSpec(dataset, rows),
		idempotencySpec(dataset, rows),
	}
}

func listSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{"space-01", "pending"},
		Dataset: dataset,
		Index:   "idx_tasks_space_status_priority_created",
		Query:   "task-list",
		Rows:    rows,
		SQL:     "SELECT id FROM tasks INDEXED BY idx_tasks_space_status_priority_created WHERE scope = 'space' AND space_id = ? AND status = ? ORDER BY priority DESC, created_at ASC, id ASC LIMIT 50",
	}
}

func statsSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{"space", "space-03", "thumbnail.generate"},
		Dataset: dataset,
		Index:   "idx_tasks_scope_space_type_status_id",
		Query:   "task-stats",
		Rows:    rows,
		SQL:     "SELECT status, COUNT(*) FROM tasks INDEXED BY idx_tasks_scope_space_type_status_id WHERE scope = ? AND space_id = ? AND type = ? GROUP BY status",
	}
}

func claimSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{"library.scan", "pending", 1893456000},
		Dataset: dataset,
		Index:   "idx_tasks_type_status_priority_created",
		Query:   "task-claim",
		Rows:    rows,
		SQL:     "SELECT id FROM tasks INDEXED BY idx_tasks_type_status_priority_created WHERE type = ? AND status = ? AND (next_run_at IS NULL OR next_run_at <= ?) ORDER BY priority DESC, created_at ASC, id ASC LIMIT 50",
	}
}

func assertBenchmarkPass(results []queryResult) error {
	var failed []string
	for _, result := range results {
		if !result.Pass {
			failed = append(failed, fmt.Sprintf("%s/%s p95=%.3fms > %.0fms", result.Dataset, result.Query, result.P95, result.ThresholdMS))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("benchmark 未达标: %s", strings.Join(failed, "; "))
	}
	return nil
}

func idempotencySpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{fmt.Sprintf("idem-%08d", rows-10)},
		Dataset: dataset,
		Index:   "idx_tasks_idempotency_active",
		Query:   "task-idempotency",
		Rows:    rows,
		SQL:     "SELECT id FROM tasks INDEXED BY idx_tasks_idempotency_active WHERE idempotency_key <> '' AND idempotency_key = ? AND status IN ('pending', 'running') LIMIT 1",
	}
}

func runQuery(db *sql.DB, dbPath string, spec querySpec) (queryResult, error) {
	for sample := 0; sample < warmupCount; sample++ {
		if _, _, err := timeQuery(db, spec); err != nil {
			return queryResult{}, fmt.Errorf("%s/%s 预热失败: %w", spec.Dataset, spec.Query, err)
		}
	}
	durations := make([]float64, 0, sampleCount)
	var rowCount int
	for sample := 0; sample < sampleCount; sample++ {
		duration, count, err := timeQuery(db, spec)
		if err != nil {
			return queryResult{}, fmt.Errorf("%s/%s 执行失败: %w", spec.Dataset, spec.Query, err)
		}
		if sample == 0 {
			rowCount = count
		}
		if count != rowCount {
			return queryResult{}, fmt.Errorf("%s/%s 返回行数不稳定：首次 %d，本次 %d", spec.Dataset, spec.Query, rowCount, count)
		}
		durations = append(durations, duration)
	}
	plan, err := explainQueryPlan(db, spec)
	if err != nil {
		return queryResult{}, fmt.Errorf("%s/%s 查询计划失败: %w", spec.Dataset, spec.Query, err)
	}
	p95 := percentile(durations, 0.95)
	threshold := backendThresholdMS(spec.Dataset)
	return queryResult{
		Args:          spec.Args,
		DatabaseBytes: fileSize(dbPath),
		Dataset:       spec.Dataset,
		Index:         spec.Index,
		IndexPlan:     plan,
		P95:           p95,
		P99:           percentile(durations, 0.99),
		Pass:          p95 <= threshold,
		Query:         spec.Query,
		Samples:       len(durations),
		ScannedRows:   int64(rowCount),
		SQL:           spec.SQL,
		ThresholdMS:   threshold,
	}, nil
}

func timeQuery(db *sql.DB, spec querySpec) (float64, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	rows, err := db.QueryContext(ctx, spec.SQL, spec.Args...)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = rows.Close()
	}()
	columns, err := rows.Columns()
	if err != nil {
		return 0, 0, err
	}
	values := make([]any, len(columns))
	for i := range values {
		var value any
		values[i] = &value
	}
	count := 0
	for rows.Next() {
		if err := rows.Scan(values...); err != nil {
			return 0, 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return float64(time.Since(started).Microseconds()) / 1000, count, nil
}

func explainQueryPlan(db *sql.DB, spec querySpec) ([]string, error) {
	rows, err := db.Query("EXPLAIN QUERY PLAN "+spec.SQL, spec.Args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			return nil, err
		}
		plan = append(plan, detail)
	}
	return plan, rows.Err()
}

func percentile(values []float64, ratio float64) float64 {
	sort.Float64s(values)
	index := int(float64(len(values))*ratio+0.999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func backendThresholdMS(dataset string) float64 {
	if dataset == "media-index-10m" {
		return 300
	}
	return 100
}

func renderSummary(r report) string {
	var b strings.Builder
	b.WriteString("# FR2-037 Task Queue Benchmark Summary\n\n")
	b.WriteString("生成时间：" + r.GeneratedAt + "\n\n")
	b.WriteString("| 数据集 | 查询 | 索引 | p95(ms) | p99(ms) | 门槛(ms) | 返回行数 | 判定 |\n")
	b.WriteString("|---|---|---|---:|---:|---:|---:|---|\n")
	for _, item := range r.Results {
		status := "达标"
		if !item.Pass {
			status = "未达标"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %.3f | %.3f | %.0f | %d | %s |\n",
			item.Dataset, item.Query, item.Index, item.P95, item.P99, item.ThresholdMS, item.ScannedRows, status))
	}
	b.WriteString("\n## SQL 与索引计划\n\n")
	for _, item := range r.Results {
		b.WriteString("### " + item.Dataset + " / " + item.Query + "\n\n")
		b.WriteString("- SQL：" + item.SQL + "\n")
		args, _ := json.Marshal(item.Args)
		b.WriteString("- 参数：" + string(args) + "\n")
		b.WriteString("- 索引：" + item.Index + "\n")
		for _, plan := range item.IndexPlan {
			b.WriteString("- 计划：" + plan + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeFailure(outputDir string, err error) {
	_ = os.MkdirAll(outputDir, 0o755)
	message := "FR2-037 benchmark 执行失败\n时间：" + time.Now().Format(time.RFC3339) + "\n原因：" + err.Error() + "\n"
	_ = os.WriteFile(filepath.Join(outputDir, "failure.txt"), []byte(message), 0o644)
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
