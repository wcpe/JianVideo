//go:build ignore

// Package main 提供 FR2-007 生产等价 SQLite 查询基准命令。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wcpe/JianVideo/internal/migration"
)

const (
	pageSize          = 50
	queryLimit        = pageSize + 1
	sampleCount       = 12
	maxOpenConns      = 8
	maxIdleConns      = 8
	librariesPerSpace = 4
	targetSpace       = "benchmark-space-a"
)

var (
	benchmarkSpaces = []string{
		"benchmark-space-a",
		"benchmark-space-b",
		"benchmark-space-c",
		"benchmark-space-d",
	}
	thresholdScale = 1.0
)

type querySpec struct {
	Args  []any  `json:"args"`
	Name  string `json:"name"`
	SQL   string `json:"sql"`
	Rows  int64  `json:"rows"`
	Label string `json:"label"`
}

type queryResult struct {
	Args              []any     `json:"args"`
	DatabaseBytes     int64     `json:"database_bytes"`
	Dataset           string    `json:"dataset"`
	DurationsMS       []float64 `json:"durations_ms"`
	ExplainPlan       []string  `json:"explain_query_plan"`
	IsolationVerified bool      `json:"isolation_verified"`
	P95               float64   `json:"p95_ms"`
	P99               float64   `json:"p99_ms"`
	Pass              bool      `json:"pass"`
	Query             string    `json:"query"`
	ResultCount       int       `json:"result_count"`
	ResultIDs         []int64   `json:"result_ids"`
	SQL               string    `json:"sql"`
	TargetSpace       string    `json:"target_space"`
	ThresholdMS       float64   `json:"threshold_ms"`
}

type environment struct {
	Driver       string           `json:"driver"`
	DSNOptions   string           `json:"dsn_options"`
	Indexes      []string         `json:"indexes"`
	MaxIdleConns int              `json:"max_idle_conns"`
	MaxOpenConns int              `json:"max_open_conns"`
	SchemaSource string           `json:"schema_source"`
	SpaceRows    map[string]int64 `json:"space_rows"`
}

type report struct {
	Environment environment   `json:"environment"`
	GeneratedAt string        `json:"generated_at"`
	GitCommit   string        `json:"git_commit"`
	Results     []queryResult `json:"results"`
}

type indexDefinition struct {
	Name string
	SQL  string
}

func main() {
	rowsText := flag.String("rows", "1000000,5000000,10000000", "逗号分隔的数据集行数")
	outputFlag := flag.String("output-dir", "", "报告输出目录，默认写入 .tmp/benchmark/fr2-007/时间戳")
	thresholdScaleFlag := flag.Float64("threshold-scale", 1, "仅用于更严格门禁回归，取值范围 (0,1]")
	flag.Parse()

	if *thresholdScaleFlag <= 0 || *thresholdScaleFlag > 1 {
		exit(fmt.Errorf("阈值缩放必须大于 0 且不超过 1"))
	}
	thresholdScale = *thresholdScaleFlag
	datasetRows, err := parseDatasetRows(*rowsText)
	if err != nil {
		exit(err)
	}
	outputDir := *outputFlag
	if outputDir == "" {
		outputDir = filepath.Join(".tmp", "benchmark", "fr2-007", time.Now().Format("20060102-150405"))
	}
	if err := run(outputDir, datasetRows); err != nil {
		writeFailure(outputDir, err)
		exit(err)
	}
	fmt.Println("FR2-007 benchmark 报告路径：" + outputDir)
}

func run(outputDir string, datasetRows []int64) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join(outputDir, "sqlite-production.db")
	gdb, sqlDB, err := openProductionDatabase(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	if err := applyProductionSchema(gdb); err != nil {
		return err
	}
	if err := seedLibraries(sqlDB); err != nil {
		return err
	}
	mediaIndexes, err := mediaIndexDefinitions(sqlDB)
	if err != nil {
		return err
	}

	results := make([]queryResult, 0, len(datasetRows)*3)
	for _, rows := range datasetRows {
		if err := dropIndexes(sqlDB, mediaIndexes); err != nil {
			return err
		}
		if err := seedMediaUntil(sqlDB, rows); err != nil {
			return err
		}
		if err := createIndexes(sqlDB, mediaIndexes); err != nil {
			return err
		}
		if _, err := sqlDB.Exec("ANALYZE"); err != nil {
			return err
		}
		batch, err := runDataset(sqlDB, dbPath, rows)
		if err != nil {
			return err
		}
		results = append(results, batch...)
	}
	indexes, err := listIndexes(sqlDB)
	if err != nil {
		return err
	}
	spaceRows, err := countSpaceRows(sqlDB)
	if err != nil {
		return err
	}
	r := report{
		Environment: environment{
			Driver:       "github.com/mattn/go-sqlite3（经 gorm sqlite）",
			DSNOptions:   "_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=on",
			Indexes:      indexes,
			MaxIdleConns: maxIdleConns,
			MaxOpenConns: maxOpenConns,
			SchemaSource: "internal/migration.DefaultMigrations",
			SpaceRows:    spaceRows,
		},
		GeneratedAt: time.Now().Format(time.RFC3339),
		GitCommit:   gitCommit(),
		Results:     results,
	}
	if err := writeJSON(filepath.Join(outputDir, "results.json"), r); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "summary.md"), []byte(renderSummary(r)), 0o644); err != nil {
		return err
	}
	if failed := failedResults(results); len(failed) > 0 {
		return fmt.Errorf("基准未达阈值，阻断交付：%s", strings.Join(failed, "、"))
	}
	return nil
}

func openProductionDatabase(dbPath string) (*gorm.DB, *sql.DB, error) {
	dsn := productionDSN(dbPath)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return gdb, sqlDB, nil
}

func productionDSN(dbPath string) string {
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	return dbPath + separator + "_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=on"
}

func applyProductionSchema(db *gorm.DB) error {
	ctx := context.Background()
	for _, step := range migration.DefaultMigrations() {
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return step.Up(ctx, tx)
		}); err != nil {
			return fmt.Errorf("应用生产迁移 %s 失败: %w", step.ID, err)
		}
		if step.Validate != nil {
			if _, err := step.Validate(ctx, db); err != nil {
				return fmt.Errorf("校验生产迁移 %s 失败: %w", step.ID, err)
			}
		}
	}
	return nil
}

func seedLibraries(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for spaceIndex, spaceID := range benchmarkSpaces {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO spaces(id, name, owner_user_id, created_at, updated_at)
			VALUES (?, ?, 1, datetime('now'), datetime('now'))`, spaceID, "Benchmark Space "+strconv.Itoa(spaceIndex+1)); err != nil {
			return err
		}
		for libraryOffset := 1; libraryOffset <= librariesPerSpace; libraryOffset++ {
			libraryID := spaceIndex*librariesPerSpace + libraryOffset
			path := fmt.Sprintf("/benchmark/%s/lib-%03d", spaceID, libraryOffset)
			if _, err := tx.Exec(`INSERT INTO library_paths(
				id, space_id, path, type, library_kind, library_profile_json, label, enabled, created_at
			) VALUES (?, ?, ?, 'local', 'mixed', '{}', ?, 1, datetime('now'))`,
				libraryID, spaceID, path, fmt.Sprintf("基准库-%03d", libraryOffset)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func seedMediaUntil(db *sql.DB, target int64) error {
	var current int64
	if err := db.QueryRow("SELECT COUNT(*) FROM media_files").Scan(&current); err != nil {
		return err
	}
	const batchSize int64 = 100_000
	for current < target {
		batchEnd := current + batchSize
		if batchEnd > target {
			batchEnd = target
		}
		if _, err := db.Exec(`WITH RECURSIVE seq(id) AS (
			SELECT ? UNION ALL SELECT id + 1 FROM seq WHERE id < ?
		)
		INSERT INTO media_files(
			id, space_id, library_id, file_path, file_name, file_size, format,
			file_state, added_at, modified_at, media_time, deleted_at, content_hash_stale
		)
		SELECT
			id,
			CASE ((id - 1) % 4)
				WHEN 0 THEN 'benchmark-space-a'
				WHEN 1 THEN 'benchmark-space-b'
				WHEN 2 THEN 'benchmark-space-c'
				ELSE 'benchmark-space-d'
			END,
			(((id - 1) % 4) * 4) + ((((id - 1) / 4) % 4) + 1),
			printf('/benchmark/benchmark-space-%s/lib-%03d/item-%08d.%s',
				char(97 + ((id - 1) % 4)), ((((id - 1) / 4) % 4) + 1), id,
				CASE WHEN id % 4 = 0 THEN 'jpg' ELSE 'mp4' END),
			printf('item-%08d', id),
			(id % 50000000) + 1024,
			CASE WHEN id % 4 = 0 THEN 'jpg' ELSE 'mp4' END,
			'available',
			datetime(1893456000 - id, 'unixepoch'),
			datetime(1893456000 - id, 'unixepoch'),
			datetime(1893456000 - id, 'unixepoch'),
			CASE WHEN id % 97 = 0 THEN datetime(1893456000 - id, 'unixepoch') ELSE NULL END,
			1
		FROM seq`, current+1, batchEnd); err != nil {
			return fmt.Errorf("生成 %d 到 %d 行数据失败: %w", current+1, batchEnd, err)
		}
		current = batchEnd
	}
	return nil
}

func runDataset(db *sql.DB, dbPath string, rows int64) ([]queryResult, error) {
	specs := buildSpecs(rows)
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

func buildSpecs(rows int64) []querySpec {
	cursorID := rows / 10
	if cursorID < 4 {
		cursorID = 4
	}
	cursorTime := time.Unix(1893456000-cursorID, 0).UTC().Format("2006-01-02 15:04:05")
	active := "deleted_at IS NULL AND (file_state IS NULL OR file_state = '' OR file_state = 'available')"
	return []querySpec{
		{
			Args:  []any{targetSpace, cursorTime, cursorTime, cursorID, queryLimit},
			Name:  "space-time-page",
			Rows:  rows,
			Label: "Space + 时间游标分页",
			SQL:   "SELECT id FROM media_files WHERE space_id = ? AND " + active + " AND (added_at < ? OR (added_at = ? AND id < ?)) ORDER BY added_at DESC, id DESC LIMIT ?",
		},
		{
			Args:  []any{targetSpace, int64(1), "/benchmark/benchmark-space-a/lib-001/%", queryLimit},
			Name:  "path-prefix-page",
			Rows:  rows,
			Label: "Space + 媒体库 + 路径前缀分页",
			SQL:   "SELECT id FROM media_files WHERE space_id = ? AND " + active + " AND library_id = ? AND file_path LIKE ? ORDER BY added_at DESC, id DESC LIMIT ?",
		},
		{
			Args:  []any{targetSpace, "mp4", cursorTime, cursorTime, cursorID, queryLimit},
			Name:  "filter-combination-page",
			Rows:  rows,
			Label: "Space + 格式筛选 + 时间游标分页",
			SQL:   "SELECT id FROM media_files WHERE space_id = ? AND " + active + " AND LOWER(format) IN (?) AND (added_at < ? OR (added_at = ? AND id < ?)) ORDER BY added_at DESC, id DESC LIMIT ?",
		},
	}
}

func runQuery(db *sql.DB, dbPath string, spec querySpec) (queryResult, error) {
	durations := make([]float64, 0, sampleCount)
	var resultIDs []int64
	for sample := 0; sample < sampleCount; sample++ {
		duration, ids, err := timeQuery(db, spec)
		if err != nil {
			return queryResult{}, err
		}
		if len(ids) != queryLimit {
			return queryResult{}, fmt.Errorf("%s/%s 返回 %d 条，期望 %d 条", datasetName(spec.Rows), spec.Name, len(ids), queryLimit)
		}
		if sample == 0 {
			resultIDs = ids
		} else if !equalIDs(resultIDs, ids) {
			return queryResult{}, fmt.Errorf("%s/%s 多次查询结果不稳定", datasetName(spec.Rows), spec.Name)
		}
		durations = append(durations, duration)
	}
	plan, err := explainQueryPlan(db, spec)
	if err != nil {
		return queryResult{}, err
	}
	isolationVerified, err := verifyResultIsolation(db, targetSpace, resultIDs)
	if err != nil {
		return queryResult{}, err
	}
	p95 := percentile(append([]float64(nil), durations...), 0.95)
	threshold := backendThresholdMS(spec.Rows, spec.Name)
	return queryResult{
		Args:              spec.Args,
		DatabaseBytes:     fileSize(dbPath),
		Dataset:           datasetName(spec.Rows),
		DurationsMS:       durations,
		ExplainPlan:       plan,
		IsolationVerified: isolationVerified,
		P95:               p95,
		P99:               percentile(append([]float64(nil), durations...), 0.99),
		Pass:              p95 <= threshold && isolationVerified,
		Query:             spec.Label,
		ResultCount:       len(resultIDs),
		ResultIDs:         resultIDs,
		SQL:               spec.SQL,
		TargetSpace:       targetSpace,
		ThresholdMS:       threshold,
	}, nil
}

func timeQuery(db *sql.DB, spec querySpec) (float64, []int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	rows, err := db.QueryContext(ctx, spec.SQL, spec.Args...)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, queryLimit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	return float64(time.Since(started).Microseconds()) / 1000, ids, nil
}

func explainQueryPlan(db *sql.DB, spec querySpec) ([]string, error) {
	rows, err := db.Query("EXPLAIN QUERY PLAN "+spec.SQL, spec.Args...)
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

func parseDatasetRows(text string) ([]int64, error) {
	parts := strings.Split(text, ",")
	rows := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("数据集行数无效：%q", part)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		rows = append(rows, value)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("至少需要一个数据集行数")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i] < rows[j] })
	return rows, nil
}

func mediaIndexDefinitions(db *sql.DB) ([]indexDefinition, error) {
	rows, err := db.Query(`SELECT name, sql FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'media_files' AND sql IS NOT NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var definitions []indexDefinition
	for rows.Next() {
		var definition indexDefinition
		if err := rows.Scan(&definition.Name, &definition.SQL); err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func dropIndexes(db *sql.DB, definitions []indexDefinition) error {
	for _, definition := range definitions {
		name := strings.ReplaceAll(definition.Name, `"`, `""`)
		if _, err := db.Exec(`DROP INDEX IF EXISTS "` + name + `"`); err != nil {
			return fmt.Errorf("临时移除生产索引 %s 失败: %w", definition.Name, err)
		}
	}
	return nil
}

func createIndexes(db *sql.DB, definitions []indexDefinition) error {
	for _, definition := range definitions {
		if _, err := db.Exec(definition.SQL); err != nil {
			return fmt.Errorf("恢复生产索引 %s 失败: %w", definition.Name, err)
		}
	}
	return nil
}

func countSpaceRows(db *sql.DB) (map[string]int64, error) {
	rows, err := db.Query("SELECT space_id, COUNT(*) FROM media_files GROUP BY space_id ORDER BY space_id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int64, len(benchmarkSpaces))
	for rows.Next() {
		var spaceID string
		var count int64
		if err := rows.Scan(&spaceID, &count); err != nil {
			return nil, err
		}
		counts[spaceID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, spaceID := range benchmarkSpaces {
		if counts[spaceID] == 0 {
			return nil, fmt.Errorf("多 Space 数据集校验失败：%s 没有媒体数据", spaceID)
		}
	}
	return counts, nil
}

func verifyResultIsolation(db *sql.DB, spaceID string, ids []int64) (bool, error) {
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, spaceID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := "SELECT COUNT(*) FROM media_files WHERE space_id = ? AND id IN (" + strings.Join(placeholders, ",") + ")"
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, err
	}
	return count == len(ids), nil
}

func failedResults(results []queryResult) []string {
	failed := make([]string, 0)
	for _, result := range results {
		if result.Pass {
			continue
		}
		failed = append(failed, fmt.Sprintf("%s/%s(p95=%.3fms, 阈值=%.0fms, Space隔离=%t)",
			result.Dataset, result.Query, result.P95, result.ThresholdMS, result.IsolationVerified))
	}
	return failed
}

func gitCommit() string {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func listIndexes(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name IN ('media_files', 'library_paths') ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		indexes = append(indexes, name)
	}
	return indexes, rows.Err()
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

func backendThresholdMS(rows int64, query string) float64 {
	threshold := 300.0
	if query == "space-time-page" {
		threshold = 200
	}
	if rows == 10_000_000 {
		threshold = 800
		if query == "space-time-page" {
			threshold = 500
		}
	}
	return threshold * thresholdScale
}

func datasetName(rows int64) string {
	if rows%1_000_000 == 0 {
		return fmt.Sprintf("media-index-%dm", rows/1_000_000)
	}
	return fmt.Sprintf("media-index-%d", rows)
}

func equalIDs(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func renderSummary(r report) string {
	var b strings.Builder
	b.WriteString("# FR2-007 SQLite Benchmark 摘要\n\n")
	b.WriteString("生成时间：" + r.GeneratedAt + "\n\n")
	b.WriteString("源码 HEAD：" + r.GitCommit + "\n\n")
	b.WriteString(fmt.Sprintf("- 驱动：%s\n- DSN 参数：%s\n- 连接池：max_open=%d，max_idle=%d\n- Schema 真源：%s\n- 查询未使用 INDEXED BY 等强制索引提示\n\n",
		r.Environment.Driver, r.Environment.DSNOptions, r.Environment.MaxOpenConns, r.Environment.MaxIdleConns, r.Environment.SchemaSource))
	b.WriteString("## Space 数据分布\n\n")
	for _, spaceID := range benchmarkSpaces {
		b.WriteString(fmt.Sprintf("- %s：%d 行\n", spaceID, r.Environment.SpaceRows[spaceID]))
	}
	b.WriteString("\n| 数据集 | 查询 | 目标 Space | 隔离验证 | p95(ms) | p99(ms) | 门槛(ms) | 结果数 | 首/末 ID | 判定 |\n")
	b.WriteString("|---|---|---|---|---:|---:|---:|---:|---|---|\n")
	for _, item := range r.Results {
		status := "达标"
		if !item.Pass {
			status = "未达标"
		}
		first, last := int64(0), int64(0)
		if len(item.ResultIDs) > 0 {
			first, last = item.ResultIDs[0], item.ResultIDs[len(item.ResultIDs)-1]
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %t | %.3f | %.3f | %.0f | %d | %d / %d | %s |\n",
			item.Dataset, item.Query, item.TargetSpace, item.IsolationVerified, item.P95, item.P99,
			item.ThresholdMS, item.ResultCount, first, last, status))
	}
	b.WriteString("\n## EXPLAIN QUERY PLAN 与实测样本\n\n")
	for _, item := range r.Results {
		b.WriteString("### " + item.Dataset + " / " + item.Query + "\n\n")
		b.WriteString("- SQL：" + item.SQL + "\n")
		args, _ := json.Marshal(item.Args)
		b.WriteString("- 参数：" + string(args) + "\n")
		durations, _ := json.Marshal(item.DurationsMS)
		b.WriteString("- 真实耗时(ms)：" + string(durations) + "\n")
		ids, _ := json.Marshal(item.ResultIDs)
		b.WriteString("- 真实结果 ID：" + string(ids) + "\n")
		for _, plan := range item.ExplainPlan {
			b.WriteString("- 查询计划：" + plan + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## 生产索引列表\n\n")
	for _, index := range r.Environment.Indexes {
		b.WriteString("- " + index + "\n")
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
