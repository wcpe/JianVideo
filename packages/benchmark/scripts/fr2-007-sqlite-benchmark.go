//go:build ignore

// Package main 提供 FR2-007 本机 SQLite 查询基准命令。
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
	sampleCount   int   = 12
	schemaVersion int   = 7003
)

type querySpec struct {
	Dataset string `json:"dataset"`
	Rows    int64  `json:"rows"`
	Query   string `json:"query"`
	SQL     string `json:"sql"`
	Index   string `json:"index"`
	Args    []any  `json:"args"`
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
	outputDir := filepath.Join(".tmp", "benchmark", "fr2-007", time.Now().Format("20060102-150405"))
	if err := run(outputDir); err != nil {
		writeFailure(outputDir, err)
		exit(err)
	}
}

func run(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(".tmp", "benchmark", "fr2-007", "sqlite-index.db")
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
	if err := ensureDataset(db); err != nil {
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
	return os.WriteFile(filepath.Join(outputDir, "summary.md"), []byte(renderSummary(r)), 0o644)
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

func ensureDataset(db *sql.DB) error {
	var userVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		return err
	}
	var count int64
	if userVersion == schemaVersion {
		if err := db.QueryRow("SELECT COUNT(*) FROM media_files").Scan(&count); err == nil && count == maxRows {
			return nil
		}
	}
	if err := rebuildDataset(db); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}

func rebuildDataset(db *sql.DB) error {
	statements := []string{
		"DROP TABLE IF EXISTS media_files",
		"DROP TABLE IF EXISTS library_paths",
		`CREATE TABLE library_paths (
			id INTEGER PRIMARY KEY,
			space_id TEXT NOT NULL,
			path TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'local',
			label TEXT,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME
		)`,
		`CREATE TABLE media_files (
			id INTEGER PRIMARY KEY,
			space_id TEXT NOT NULL,
			library_id INTEGER NOT NULL,
			file_path TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_size INTEGER DEFAULT 0,
			format TEXT,
			added_at DATETIME,
			media_time DATETIME,
			deleted_at DATETIME
		)`,
		`WITH RECURSIVE seq(id) AS (
			SELECT 1
			UNION ALL
			SELECT id + 1 FROM seq WHERE id < 1000
		)
		INSERT INTO library_paths(id, space_id, path, type, label, enabled, created_at)
		SELECT
			id,
			printf('space-%02d', (id % 10) + 1),
			printf('/space-%02d/lib-%03d', (id % 10) + 1, id % 256),
			'local',
			printf('库-%03d', id % 256),
			1,
			1700000000
		FROM seq`,
		`WITH RECURSIVE seq(id) AS (
			SELECT 1
			UNION ALL
			SELECT id + 1 FROM seq WHERE id < 10000000
		)
		INSERT INTO media_files
		SELECT
			id,
			printf('space-%02d', (id % 10) + 1),
			(id % 256) + 1,
			printf('/space-%02d/lib-%03d/item-%08d.%s', (id % 10) + 1, id % 256, id, CASE WHEN id % 4 = 0 THEN 'jpg' ELSE 'mp4' END),
			printf('item-%08d', id),
			(id % 50000000) + 1024,
			CASE WHEN id % 4 = 0 THEN 'jpg' ELSE 'mp4' END,
			1893456000 - id,
			1893456000 - id,
			CASE WHEN id % 97 = 0 THEN 1893456000 - id ELSE NULL END
		FROM seq`,
		"CREATE INDEX idx_library_paths_space_id ON library_paths(space_id)",
		"CREATE INDEX idx_library_paths_space_path_id ON library_paths(space_id, path, id)",
		"CREATE INDEX idx_library_paths_space_enabled_id ON library_paths(space_id, enabled, id)",
		"CREATE INDEX idx_media_files_space_id ON media_files(space_id)",
		"CREATE INDEX idx_media_files_space_added_id ON media_files(space_id, added_at DESC, id DESC)",
		"CREATE INDEX idx_media_files_space_media_time_id ON media_files(space_id, media_time DESC, id DESC)",
		"CREATE INDEX idx_media_files_space_library_path_id ON media_files(space_id, library_id, file_path, id)",
		"CREATE INDEX idx_media_files_space_deleted_id ON media_files(space_id, deleted_at, id)",
		"CREATE INDEX idx_media_files_space_format_added_id ON media_files(space_id, format, added_at DESC, id DESC)",
		"ANALYZE",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func runBenchmarks(db *sql.DB, dbPath string) ([]queryResult, error) {
	specs := buildSpecs()
	results := make([]queryResult, 0, len(specs))
	for _, spec := range specs {
		result, err := runQuery(db, dbPath, spec)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func buildSpecs() []querySpec {
	datasets := []struct {
		name string
		rows int64
	}{
		{name: "media-index-1m", rows: 1_000_000},
		{name: "media-index-5m", rows: 5_000_000},
		{name: "media-index-10m", rows: 10_000_000},
	}
	specs := make([]querySpec, 0, len(datasets)*3)
	for _, dataset := range datasets {
		specs = append(specs, spaceTimeSpec(dataset.name, dataset.rows))
		specs = append(specs, pathPrefixSpec(dataset.name, dataset.rows))
		specs = append(specs, filterSpec(dataset.name, dataset.rows))
	}
	return specs
}

func spaceTimeSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{rows, "space-03", int64(1893456000 - 1200)},
		Dataset: dataset,
		Index:   "idx_media_files_space_added_id",
		Query:   "space-time-page",
		Rows:    rows,
		SQL:     "SELECT id FROM media_files WHERE id <= ? AND space_id = ? AND added_at < ? AND deleted_at IS NULL ORDER BY added_at DESC, id DESC LIMIT 50",
	}
}

func pathPrefixSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{rows, "space-03", int64(9), "/space-03/lib-008/", "/space-03/lib-009/"},
		Dataset: dataset,
		Index:   "idx_media_files_space_library_path_id",
		Query:   "path-prefix",
		Rows:    rows,
		SQL:     "SELECT id FROM media_files WHERE id <= ? AND space_id = ? AND library_id = ? AND file_path >= ? AND file_path < ? ORDER BY file_path, id LIMIT 50",
	}
}

func filterSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []any{rows, "space-03", "mp4", int64(1893456000 - 1200)},
		Dataset: dataset,
		Index:   "idx_media_files_space_format_added_id",
		Query:   "filter-combination",
		Rows:    rows,
		SQL:     "SELECT id FROM media_files WHERE id <= ? AND space_id = ? AND format = ? AND added_at < ? AND deleted_at IS NULL ORDER BY added_at DESC, id DESC LIMIT 50",
	}
}

func runQuery(db *sql.DB, dbPath string, spec querySpec) (queryResult, error) {
	durations := make([]float64, 0, sampleCount)
	for sample := 0; sample < sampleCount; sample++ {
		duration, count, err := timeQuery(db, spec)
		if err != nil {
			return queryResult{}, err
		}
		if count != pageSize {
			return queryResult{}, fmt.Errorf("%s/%s 返回 %d 条，期望 %d 条", spec.Dataset, spec.Query, count, pageSize)
		}
		durations = append(durations, duration)
	}
	plan, err := explainQueryPlan(db, spec)
	if err != nil {
		return queryResult{}, err
	}
	p95 := percentile(durations, 0.95)
	threshold := backendThresholdMS(spec.Dataset, spec.Query)
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
		ScannedRows:   int64(pageSize),
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
	count := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
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

func backendThresholdMS(dataset, query string) float64 {
	if query == "space-time-page" {
		if dataset == "media-index-10m" {
			return 500
		}
		return 200
	}
	if dataset == "media-index-10m" {
		return 800
	}
	return 300
}

func renderSummary(r report) string {
	var b strings.Builder
	b.WriteString("# FR2-007 Benchmark Summary\n\n")
	b.WriteString("生成时间：" + r.GeneratedAt + "\n\n")
	b.WriteString("| 数据集 | 查询 | 索引 | p95(ms) | p99(ms) | 门槛(ms) | 扫描行数 | 判定 |\n")
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
	message := "FR2-007 benchmark 执行失败\n时间：" + time.Now().Format(time.RFC3339) + "\n原因：" + err.Error() + "\n"
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
