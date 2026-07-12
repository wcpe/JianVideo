//go:build ignore

// Package main 提供 FR2-030 文件自带元数据 SQLite 体积基准命令。
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const benchmarkRows = 10_000

type result struct {
	BaselineBytes int64   `json:"baseline_bytes"`
	DatabaseBytes int64   `json:"database_bytes"`
	GrowthBytes   int64   `json:"growth_bytes"`
	GrowthPerRow  float64 `json:"growth_per_row_bytes"`
	RawJSONBytes  int     `json:"raw_json_bytes"`
	Rows          int     `json:"rows"`
}

type report struct {
	GeneratedAt string `json:"generated_at"`
	Result      result `json:"result"`
}

func main() {
	outputDir := filepath.Join(".tmp", "benchmark", "fr2-030", time.Now().Format("20060102-150405"))
	if err := run(outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(outputDir, "metadata-size.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if err := createSchema(db); err != nil {
		return err
	}
	baseline := fileSize(dbPath)
	rawJSON := sampleRawJSON()
	if err := insertRows(db, rawJSON); err != nil {
		return err
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return err
	}
	measured := fileSize(dbPath)
	r := report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Result: result{
			BaselineBytes: baseline,
			DatabaseBytes: measured,
			GrowthBytes:   measured - baseline,
			GrowthPerRow:  float64(measured-baseline) / benchmarkRows,
			RawJSONBytes:  len(rawJSON),
			Rows:          benchmarkRows,
		},
	}
	if err := writeJSON(filepath.Join(outputDir, "results.json"), r); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "summary.md"), []byte(renderSummary(r)), 0o644)
}

func createSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE media_metadata (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			media_id INTEGER NOT NULL,
			space_id TEXT NOT NULL,
			source TEXT NOT NULL,
			tool TEXT NOT NULL,
			tool_version TEXT NOT NULL,
			raw_json TEXT NOT NULL,
			normalized_json TEXT NOT NULL,
			parsed_at DATETIME NOT NULL,
			stale INTEGER NOT NULL DEFAULT 0
		)`,
		"CREATE UNIQUE INDEX idx_media_metadata_space_media_source ON media_metadata(space_id, media_id, source)",
		"CREATE INDEX idx_media_metadata_media ON media_metadata(space_id, media_id)",
		"CREATE INDEX idx_media_metadata_space_stale ON media_metadata(space_id, stale)",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func insertRows(db *sql.DB, rawJSON string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.Prepare(`INSERT INTO media_metadata(media_id, space_id, source, tool, tool_version, raw_json, normalized_json, parsed_at, stale) VALUES (?, ?, 'ffprobe', 'ffprobe', '7.1', ?, ?, ?, 0)`)
	if err != nil {
		return err
	}
	defer func() { _ = statement.Close() }()
	normalizedJSON := `{"container":{"format_name":"mov,mp4","duration":120.5},"video_streams":[{"codec_name":"h264","width":3840,"height":2160,"frame_rate":"30000/1001"}],"audio_streams":[{"codec_name":"aac","language":"zh"}]}`
	parsedAt := time.Now().UTC()
	for id := 1; id <= benchmarkRows; id++ {
		if _, err := statement.Exec(id, fmt.Sprintf("space-%02d", id%10), rawJSON, normalizedJSON, parsedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sampleRawJSON() string {
	padding := strings.Repeat("元数据字段", 256)
	return fmt.Sprintf(`{"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","tags":{"title":"示例视频","comment":%q}},"streams":[{"index":0,"codec_name":"h264","width":3840,"height":2160,"r_frame_rate":"30000/1001","avg_frame_rate":"24000/1001"},{"index":1,"codec_name":"aac","channels":2,"tags":{"language":"zh"}}]}`, padding)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderSummary(r report) string {
	return fmt.Sprintf("# FR2-030 Metadata SQLite Size Benchmark\n\n- 记录数：%d\n- 单条 raw JSON：%d bytes\n- 空表与索引：%d bytes\n- 写入后数据库：%d bytes\n- 净增长：%d bytes\n- 平均每条增长：%.2f bytes\n", r.Result.Rows, r.Result.RawJSONBytes, r.Result.BaselineBytes, r.Result.DatabaseBytes, r.Result.GrowthBytes, r.Result.GrowthPerRow)
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
