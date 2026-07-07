package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	maxRows       int64 = 10_000_000
	pageSize            = 50
	sampleCount         = 8
	schemaVersion       = 6302
)

type querySpec struct {
	Dataset string        `json:"dataset"`
	Rows    int64         `json:"rows"`
	Query   string        `json:"query"`
	SQL     string        `json:"sql"`
	Args    []interface{} `json:"-"`
}

type queryResult struct {
	DatabaseBytes int64   `json:"databaseBytes"`
	Dataset       string  `json:"dataset"`
	Index         string  `json:"index"`
	P95           float64 `json:"p95"`
	Query         string  `json:"query"`
	Samples       int     `json:"samples"`
	ScannedRows   int64   `json:"scannedRows"`
	SQL           string  `json:"sql"`
}

func main() {
	dbPath := filepath.Join(".tmp", "benchmark", "fr2-063", "sqlite-index.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		exit(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := configure(db); err != nil {
		exit(err)
	}
	if err := ensureDataset(db); err != nil {
		exit(err)
	}

	results, err := runBenchmarks(db, dbPath)
	if err != nil {
		exit(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		exit(err)
	}
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
		if err := db.QueryRow("SELECT COUNT(*) FROM media").Scan(&count); err == nil && count == maxRows {
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
		"DROP TABLE IF EXISTS media",
		`CREATE TABLE media (
			id INTEGER PRIMARY KEY,
			space_id TEXT NOT NULL,
			media_time INTEGER NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL,
			transcode_status TEXT NOT NULL,
			ai_status TEXT NOT NULL,
			task_status TEXT NOT NULL,
			task_priority INTEGER NOT NULL
		)`,
		`WITH RECURSIVE seq(id) AS (
			SELECT 1
			UNION ALL
			SELECT id + 1 FROM seq WHERE id < 10000000
		)
		INSERT INTO media
		SELECT
			id,
			printf('space-%02d', (id % 10) + 1),
			1893456000 - id,
			printf('/space-%02d/lib-%03d/item-%08d.mp4', (id % 10) + 1, (id / 1000) % 256, id),
			CASE WHEN id % 4 = 0 THEN 'image' ELSE 'video' END,
			CASE WHEN id % 3 = 0 THEN 'ready' WHEN id % 3 = 1 THEN 'queued' ELSE 'failed' END,
			CASE WHEN id % 5 < 3 THEN 'indexed' ELSE 'pending' END,
			CASE WHEN id % 7 = 0 THEN 'queued' WHEN id % 11 = 0 THEN 'failed' ELSE 'done' END,
			id % 100
		FROM seq`,
		"CREATE INDEX idx_media_space_time ON media(space_id, media_time DESC, id DESC)",
		"CREATE INDEX idx_media_space_path ON media(space_id, path, id)",
		"CREATE INDEX idx_media_filter ON media(space_id, kind, transcode_status, ai_status, media_time DESC, id DESC)",
		"CREATE INDEX idx_media_task ON media(task_status, task_priority DESC, id)",
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
	specs := make([]querySpec, 0, len(datasets)*4)
	for _, dataset := range datasets {
		specs = append(specs, spaceTimeSpec(dataset.name, dataset.rows))
		specs = append(specs, pathPrefixSpec(dataset.name, dataset.rows))
		specs = append(specs, filterSpec(dataset.name, dataset.rows))
		specs = append(specs, taskQueueSpec(dataset.name, dataset.rows))
	}
	return specs
}

func spaceTimeSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []interface{}{rows, "space-03", int64(1893456000 - 1200)},
		Dataset: dataset,
		Query:   "space-time-page",
		Rows:    rows,
		SQL:     "SELECT id FROM media INDEXED BY idx_media_space_time WHERE id <= ? AND space_id = ? AND media_time < ? ORDER BY media_time DESC, id DESC LIMIT 50",
	}
}

func pathPrefixSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []interface{}{rows, "space-03", "/space-03/lib-007/", "/space-03/lib-008/"},
		Dataset: dataset,
		Query:   "path-prefix",
		Rows:    rows,
		SQL:     "SELECT id FROM media INDEXED BY idx_media_space_path WHERE id <= ? AND space_id = ? AND path >= ? AND path < ? ORDER BY path, id LIMIT 50",
	}
}

func filterSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []interface{}{rows, "space-03", "video", "failed", "indexed", int64(1893456000 - 1200)},
		Dataset: dataset,
		Query:   "filter-combination",
		Rows:    rows,
		SQL:     "SELECT id FROM media INDEXED BY idx_media_filter WHERE id <= ? AND space_id = ? AND kind = ? AND transcode_status = ? AND ai_status = ? AND media_time < ? ORDER BY media_time DESC, id DESC LIMIT 50",
	}
}

func taskQueueSpec(dataset string, rows int64) querySpec {
	return querySpec{
		Args:    []interface{}{rows, "queued"},
		Dataset: dataset,
		Query:   "task-queue",
		Rows:    rows,
		SQL:     "SELECT id FROM media INDEXED BY idx_media_task WHERE id <= ? AND task_status = ? ORDER BY task_priority DESC, id LIMIT 50",
	}
}

func runQuery(db *sql.DB, dbPath string, spec querySpec) (queryResult, error) {
	durations := make([]float64, 0, sampleCount)
	for sample := 0; sample < sampleCount; sample += 1 {
		duration, count, err := timeQuery(db, spec)
		if err != nil {
			return queryResult{}, err
		}
		if count != pageSize {
			return queryResult{}, fmt.Errorf("%s/%s 返回 %d 条，期望 %d 条", spec.Dataset, spec.Query, count, pageSize)
		}
		durations = append(durations, duration)
	}
	databaseBytes := fileSize(dbPath)
	return queryResult{
		DatabaseBytes: databaseBytes,
		Dataset:       spec.Dataset,
		Index:         queryIndex(spec.Query),
		P95:           p95(durations),
		Query:         spec.Query,
		Samples:       len(durations),
		ScannedRows:   pageSize,
		SQL:           spec.SQL,
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
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		count += 1
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return float64(time.Since(started).Microseconds()) / 1000, count, nil
}

func queryIndex(query string) string {
	switch query {
	case "space-time-page":
		return "idx_media_space_time"
	case "path-prefix":
		return "idx_media_space_path"
	case "filter-combination":
		return "idx_media_filter"
	case "task-queue":
		return "idx_media_task"
	default:
		return "unknown"
	}
}

func p95(values []float64) float64 {
	sort.Float64s(values)
	index := int(float64(len(values))*0.95+0.999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
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
