//go:build ignore

// Package main 提供 FR2-061 内容哈希精确去重 SQLite 查询基准命令。
package main

import (
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
	sampleCount         = 12
	schemaVersion       = 6102
)

type queryResult struct {
	DatabaseBytes int64    `json:"database_bytes"`
	Dataset       string   `json:"dataset"`
	IndexPlan     []string `json:"index_plan"`
	P95           float64  `json:"p95_ms"`
	P99           float64  `json:"p99_ms"`
	Pass          bool     `json:"pass"`
	Query         string   `json:"query"`
	ReturnedRows  int      `json:"returned_rows"`
	Samples       int      `json:"samples"`
	SQL           string   `json:"sql"`
	ThresholdMS   float64  `json:"threshold_ms"`
}

type report struct {
	GeneratedAt string        `json:"generated_at"`
	Results     []queryResult `json:"results"`
}

func main() {
	outputDir := filepath.Join(".tmp", "benchmark", "fr2-061", time.Now().Format("20060102-150405"))
	if err := run(outputDir); err != nil {
		_ = os.MkdirAll(outputDir, 0o755)
		_ = os.WriteFile(filepath.Join(outputDir, "failure.txt"), []byte(err.Error()), 0o644)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(".tmp", "benchmark", "fr2-061", "file-hash-index.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
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
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = OFF",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -262144",
	} {
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
		"DROP TABLE IF EXISTS media_hash_groups",
		`CREATE TABLE media_files (
			id INTEGER PRIMARY KEY,
			space_id TEXT NOT NULL,
			library_id INTEGER NOT NULL,
			file_path TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_size INTEGER DEFAULT 0,
			format TEXT,
			content_hash TEXT DEFAULT '',
			content_hash_algo TEXT DEFAULT '',
			content_hash_computed_at DATETIME,
			content_hash_stale INTEGER NOT NULL DEFAULT 0,
			file_state TEXT NOT NULL DEFAULT 'available',
			deleted_at DATETIME
		)`,
		`CREATE TABLE media_hash_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			space_id TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			content_hash TEXT NOT NULL,
			content_hash_algo TEXT NOT NULL DEFAULT 'sha256',
			item_count INTEGER NOT NULL DEFAULT 0,
			first_media_id INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL
		)`,
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
			CASE WHEN id % 100000 IN (0, 10) THEN (id / 100000) + 4096 ELSE (id % 50000000) + 1024 END,
			CASE WHEN id % 4 = 0 THEN 'jpg' ELSE 'mp4' END,
			CASE WHEN id % 100000 IN (0, 10) THEN printf('dup-%02d-%05d', (id % 10) + 1, id / 100000) ELSE printf('hash-%016x', id) END,
			'sha256',
			1893456000 - id,
			CASE WHEN id % 997 = 0 THEN 1 ELSE 0 END,
			CASE WHEN id % 991 = 0 THEN 'missing' ELSE 'available' END,
			CASE WHEN id % 983 = 0 THEN 1893456000 - id ELSE NULL END
		FROM seq`,
		"CREATE INDEX idx_media_files_space_size_content_hash ON media_files(space_id, file_size, content_hash)",
		"CREATE INDEX idx_media_files_space_content_hash_stale ON media_files(space_id, content_hash, content_hash_stale)",
		`INSERT INTO media_hash_groups(space_id, file_size, content_hash, content_hash_algo, item_count, first_media_id, updated_at)
		SELECT space_id, file_size, content_hash, 'sha256', COUNT(*), MIN(id), datetime('now')
		FROM media_files
		WHERE deleted_at IS NULL
			AND (file_state IS NULL OR file_state = '' OR file_state = 'available')
			AND content_hash <> ''
			AND content_hash_algo = 'sha256'
			AND content_hash_stale = 0
		GROUP BY space_id, file_size, content_hash
		HAVING COUNT(*) >= 2`,
		"CREATE UNIQUE INDEX idx_media_hash_groups_space_size_hash ON media_hash_groups(space_id, file_size, content_hash)",
		"CREATE INDEX idx_media_hash_groups_space_first ON media_hash_groups(space_id, first_media_id)",
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
	datasets := []struct {
		name string
		rows int64
	}{
		{name: "media-index-1m", rows: 1_000_000},
		{name: "media-index-5m", rows: 5_000_000},
		{name: "media-index-10m", rows: 10_000_000},
	}
	results := make([]queryResult, 0, len(datasets))
	for _, dataset := range datasets {
		result, err := runExactDuplicateQuery(db, dbPath, dataset.name, dataset.rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func runExactDuplicateQuery(db *sql.DB, dbPath, dataset string, rows int64) (queryResult, error) {
	const query = `WITH candidate_keys AS (
	SELECT space_id, file_size, content_hash
	FROM media_hash_groups INDEXED BY idx_media_hash_groups_space_first
	WHERE space_id = ? AND content_hash_algo = 'sha256' AND item_count >= 2 AND first_media_id <= ?
),
duplicate_keys AS (
	SELECT candidate_keys.space_id, candidate_keys.file_size, candidate_keys.content_hash, MIN(media_files.id) AS first_media_id
	FROM candidate_keys
	JOIN media_files ON media_files.space_id = candidate_keys.space_id
		AND media_files.file_size = candidate_keys.file_size
		AND media_files.content_hash = candidate_keys.content_hash
	WHERE media_files.id <= ? AND media_files.space_id = ? AND media_files.deleted_at IS NULL
		AND (media_files.file_state IS NULL OR media_files.file_state = '' OR media_files.file_state = 'available')
		AND media_files.content_hash_algo = 'sha256' AND media_files.content_hash_stale = 0
	GROUP BY candidate_keys.space_id, candidate_keys.file_size, candidate_keys.content_hash
	HAVING COUNT(media_files.id) >= 2
	ORDER BY first_media_id ASC
	LIMIT 100
)
SELECT media_files.id, media_files.file_size, media_files.content_hash
FROM media_files
JOIN duplicate_keys ON duplicate_keys.space_id = media_files.space_id
	AND duplicate_keys.file_size = media_files.file_size
	AND duplicate_keys.content_hash = media_files.content_hash
WHERE media_files.id <= ? AND media_files.space_id = ? AND media_files.deleted_at IS NULL
	AND (media_files.file_state IS NULL OR media_files.file_state = '' OR media_files.file_state = 'available')
	AND media_files.content_hash_algo = 'sha256' AND media_files.content_hash_stale = 0
ORDER BY duplicate_keys.first_media_id ASC, media_files.id ASC`
	args := []any{"space-01", rows, rows, "space-01", rows, "space-01"}
	plan, err := explain(db, query, args...)
	if err != nil {
		return queryResult{}, err
	}
	durations := make([]float64, 0, sampleCount)
	returnedRows := 0
	for i := 0; i < sampleCount; i++ {
		start := time.Now()
		rows, err := db.Query(query, args...)
		if err != nil {
			return queryResult{}, err
		}
		count := 0
		for rows.Next() {
			var id int64
			var size int64
			var hash string
			if err := rows.Scan(&id, &size, &hash); err != nil {
				_ = rows.Close()
				return queryResult{}, err
			}
			count++
		}
		if err := rows.Close(); err != nil {
			return queryResult{}, err
		}
		durations = append(durations, float64(time.Since(start).Microseconds())/1000)
		returnedRows = count
	}
	threshold := thresholdMS(dataset)
	p95 := percentile(durations, 0.95)
	return queryResult{
		DatabaseBytes: fileSize(dbPath),
		Dataset:       dataset,
		IndexPlan:     plan,
		P95:           p95,
		P99:           percentile(durations, 0.99),
		Pass:          p95 <= threshold,
		Query:         "exact-duplicate-hash",
		ReturnedRows:  returnedRows,
		Samples:       len(durations),
		SQL:           strings.Join(strings.Fields(query), " "),
		ThresholdMS:   threshold,
	}, nil
}

func explain(db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func thresholdMS(dataset string) float64 {
	if dataset == "media-index-10m" {
		return 800
	}
	return 300
}

func percentile(values []float64, p float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * p)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderSummary(r report) string {
	var b strings.Builder
	b.WriteString("# FR2-061 File Hash Benchmark Summary\n\n")
	b.WriteString("- 生成时间：" + r.GeneratedAt + "\n")
	b.WriteString("- 查询：精确重复 `file_size + content_hash` 分组并回取成员\n")
	b.WriteString("- 阈值：按 FR2-003 筛选组合门槛，1m/5m p95 ≤ 300ms，10m p95 ≤ 800ms\n\n")
	b.WriteString("| 数据集 | 查询 | p95(ms) | p99(ms) | 门槛(ms) | 返回行数 | 数据库大小(bytes) | 判定 |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---|\n")
	for _, result := range r.Results {
		b.WriteString(fmt.Sprintf(
			"| %s | %s | %.3f | %.3f | %.0f | %d | %d | %s |\n",
			result.Dataset,
			result.Query,
			result.P95,
			result.P99,
			result.ThresholdMS,
			result.ReturnedRows,
			result.DatabaseBytes,
			passLabel(result.Pass),
		))
	}
	b.WriteString("\n## 查询计划\n\n")
	for _, result := range r.Results {
		b.WriteString("### " + result.Dataset + "\n\n")
		for _, line := range result.IndexPlan {
			b.WriteString("- `" + line + "`\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func passLabel(pass bool) string {
	if pass {
		return "达标"
	}
	return "未达标"
}
