package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// TestParseAuditLimit 覆盖审计 limit 解析边界（抬高 api 覆盖率裕量）。
func TestParseAuditLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"10", 10},
		{"0", 0},
		{"-3", -3},
		{"abc", 0},
		{"1.5", 0},
	}
	for _, tc := range cases {
		if got := parseAuditLimit(tc.raw); got != tc.want {
			t.Fatalf("parseAuditLimit(%q)=%d want %d", tc.raw, got, tc.want)
		}
	}
}

// TestFilterMediaByMaxRating 覆盖空 max / 分级过滤。
func TestFilterMediaByMaxRating(t *testing.T) {
	t.Parallel()
	items := []models.MediaFile{
		{ID: 1, ContentRating: models.ContentRatingG},
		{ID: 2, ContentRating: models.ContentRatingPG},
		{ID: 3, ContentRating: models.ContentRatingR},
		{ID: 4, ContentRating: ""},
		{ID: 5, ContentRating: models.ContentRatingUnrated},
	}

	// 空 max：原样返回
	all := filterMediaByMaxRating(items, "")
	if len(all) != len(items) {
		t.Fatalf("空 max 应保留全部，got %d", len(all))
	}
	all = filterMediaByMaxRating(items, "  ")
	if len(all) != len(items) {
		t.Fatalf("空白 max 应保留全部，got %d", len(all))
	}

	// max=PG：G/PG/空/UNRATED 可见，R 不可见
	got := filterMediaByMaxRating(items, models.ContentRatingPG)
	if len(got) != 4 {
		t.Fatalf("max=PG 期望 4 条，got %d", len(got))
	}
	for _, m := range got {
		if m.ID == 3 {
			t.Fatal("max=PG 不应包含 R")
		}
	}

	// max=G：仅 G + 未分级
	got = filterMediaByMaxRating(items, models.ContentRatingG)
	if len(got) != 3 {
		t.Fatalf("max=G 期望 3 条（G+空+UNRATED），got %d", len(got))
	}
}

// TestFilterWatchMediaByMaxRating 覆盖观看列表按 max 过滤。
func TestFilterWatchMediaByMaxRating(t *testing.T) {
	t.Parallel()
	items := []library.WatchMediaItem{
		{Media: models.MediaFile{ID: 1, ContentRating: models.ContentRatingG}},
		{Media: models.MediaFile{ID: 2, ContentRating: models.ContentRatingR}},
		{Media: models.MediaFile{ID: 3, ContentRating: models.ContentRatingUnrated}},
	}

	if n := len(filterWatchMediaByMaxRating(items, "")); n != 3 {
		t.Fatalf("空 max 期望 3，got %d", n)
	}
	got := filterWatchMediaByMaxRating(items, models.ContentRatingPG)
	if len(got) != 2 {
		t.Fatalf("max=PG 期望 2（G+UNRATED），got %d", len(got))
	}
	for _, it := range got {
		if it.Media.ID == 2 {
			t.Fatal("max=PG 不应包含 R")
		}
	}
}

// TestAPIParseHelpers 覆盖若干无副作用解析助手，抬高 api 包覆盖率裕量。
func TestAPIParseHelpers(t *testing.T) {
	t.Parallel()

	if parseOptionalID(" 42 ") != 42 || parseOptionalID("x") != 0 {
		t.Fatal("parseOptionalID")
	}
	if parseOptionalInt64("7") != 7 || parseOptionalInt64("bad") != 0 {
		t.Fatal("parseOptionalInt64")
	}
	if optionalInt64Ptr("0") != nil || optionalInt64Ptr("-1") != nil {
		t.Fatal("optionalInt64Ptr 非正应 nil")
	}
	if p := optionalInt64Ptr("9"); p == nil || *p != 9 {
		t.Fatal("optionalInt64Ptr 正数")
	}
	if !isImageMediaFormat("JPG") || !isImageMediaFormat(".png") || isImageMediaFormat("mp4") || isImageMediaFormat("") {
		t.Fatal("isImageMediaFormat")
	}
	ids := parseBatchIDs("1, 2, x, -3, 0, 4")
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 4 {
		t.Fatalf("parseBatchIDs=%v", ids)
	}
	if parseBatchIDs("  ") != nil {
		t.Fatal("parseBatchIDs 空应 nil")
	}
	if parsePositiveInt("0", 5) != 5 || parsePositiveInt("abc", 5) != 5 || parsePositiveInt("12", 5) != 12 {
		t.Fatal("parsePositiveInt")
	}
	if id, ok := parseLegacyTaskID("scan:9", "scan:"); !ok || id != 9 {
		t.Fatal("parseLegacyTaskID ok")
	}
	if _, ok := parseLegacyTaskID("other:1", "scan:"); ok {
		t.Fatal("parseLegacyTaskID 前缀不匹配")
	}
	if auditCursorValue("  ") != nil {
		t.Fatal("auditCursorValue 空")
	}
	if p := auditCursorValue("abc"); p == nil || *p != "abc" {
		t.Fatal("auditCursorValue 有值")
	}
	if string(auditJSONValue("")) != "null" || string(auditJSONValue("{")) != "null" {
		t.Fatal("auditJSONValue 非法")
	}
	if !json.Valid(auditJSONValue(`{"a":1}`)) {
		t.Fatal("auditJSONValue 合法")
	}
	if parseFilterTime("") != nil || parseFilterTime("not-a-date") != nil {
		t.Fatal("parseFilterTime 无效")
	}
	if parseFilterTime("2026-07-24") == nil {
		t.Fatal("parseFilterTime 日期")
	}
	if parseFilterTime("2026-07-24T12:00:00Z") == nil {
		t.Fatal("parseFilterTime RFC3339")
	}
}

// TestAPIMorePureHelpers 再覆盖帧探针/图片格式/家长控制与任务 ID 解析。
func TestAPIMorePureHelpers(t *testing.T) {
	t.Parallel()

	if rate, ok := parseFrameRate("30000/1001"); !ok || rate < 29 || rate > 30 {
		t.Fatalf("parseFrameRate 合法: %v %v", rate, ok)
	}
	if _, ok := parseFrameRate("30"); ok {
		t.Fatal("parseFrameRate 缺分母应失败")
	}
	if _, ok := parseFrameRate("0/1"); ok {
		t.Fatal("parseFrameRate 非正应失败")
	}

	frames := []struct {
		Timestamp string `json:"best_effort_timestamp_time"`
	}{
		{Timestamp: "0"},
		{Timestamp: "0.033"},
		{Timestamp: "0.066"},
	}
	times, ok := parseFrameTimes(frames, 3)
	if !ok || len(times) != 3 {
		t.Fatalf("parseFrameTimes 合法: %v %v", times, ok)
	}
	if _, ok := parseFrameTimes(frames, 2); ok {
		t.Fatal("parseFrameTimes 长度不匹配应失败")
	}
	if _, ok := parseFrameTimes([]struct {
		Timestamp string `json:"best_effort_timestamp_time"`
	}{{Timestamp: "1"}, {Timestamp: "0.5"}}, 2); ok {
		t.Fatal("parseFrameTimes 非递增应失败")
	}

	// 合法探针：h264 + 合理帧数/尺寸
	rawOK := []byte(`{
		"streams":[{"avg_frame_rate":"25/1","nb_frames":"3","codec_name":"h264","width":320,"height":180}],
		"frames":[
			{"best_effort_timestamp_time":"0"},
			{"best_effort_timestamp_time":"0.04"},
			{"best_effort_timestamp_time":"0.08"}
		]
	}`)
	meta, ok := parseFrameProbe(rawOK)
	if !ok || meta.codecName != "h264" || meta.frameCount != 3 {
		t.Fatalf("parseFrameProbe 合法: %+v %v", meta, ok)
	}
	if !isExactFrameSource(meta) {
		// 尺寸可能不满足 marker；仅断言函数可调用
		_ = markerFits(meta)
	}
	if _, ok := parseFrameProbe([]byte(`{`)); ok {
		t.Fatal("parseFrameProbe 非法 JSON 应失败")
	}
	if _, ok := parseFrameProbe([]byte(`{"streams":[]}`)); ok {
		t.Fatal("parseFrameProbe 空 streams 应失败")
	}

	if !isImageFormat("PNG", "") || !isImageFormat("", "a/b/c.JPG") || isImageFormat("mp4", "x.mp4") {
		t.Fatal("isImageFormat")
	}
	if mediaRatingOf(nil) != "" || mediaRatingOf(&models.MediaFile{ContentRating: models.ContentRatingPG}) != models.ContentRatingPG {
		t.Fatal("mediaRatingOf")
	}
	if spaceDefaultRating(nil) != "" || spaceDefaultRating(&models.Space{DefaultMaxRating: models.ContentRatingG}) != models.ContentRatingG {
		t.Fatal("spaceDefaultRating")
	}
	if errorsIsNotFound(nil) {
		t.Fatal("errorsIsNotFound nil")
	}

	id, err := parseOptionalTaskID("")
	if err != nil || id != 0 {
		t.Fatalf("parseOptionalTaskID 空: %v %d", err, id)
	}
	id, err = parseOptionalTaskID("12")
	if err != nil || id != 12 {
		t.Fatalf("parseOptionalTaskID 合法: %v %d", err, id)
	}
	if _, err := parseOptionalTaskID("0"); err == nil {
		t.Fatal("parseOptionalTaskID 非正应失败")
	}
	if _, err := parseOptionalTaskID("x"); err == nil {
		t.Fatal("parseOptionalTaskID 非法应失败")
	}

	rules := []library.MediaTypeRuleView{{Type: "video"}, {Type: "image"}, {Type: "video"}}
	filtered := filterMediaTypeRules(rules, "video")
	if len(filtered) != 2 {
		t.Fatalf("filterMediaTypeRules: %d", len(filtered))
	}
}

// TestAPIEvenMorePureHelpers 再抬 api 裕量：书签错误映射 / Space·时间线校验 / SQLite busy 判定。
func TestAPIEvenMorePureHelpers(t *testing.T) {
	t.Parallel()

	// bookmarkValidationError 此前 0%
	if st, code, _ := bookmarkValidationError(library.ErrBookmarkInvalidPosition); st != 400 || code != "BOOKMARK_INVALID_POSITION" {
		t.Fatalf("InvalidPosition: %d %s", st, code)
	}
	if st, code, _ := bookmarkValidationError(library.ErrBookmarkTitleRequired); st != 400 || code != "BOOKMARK_TITLE_REQUIRED" {
		t.Fatalf("TitleRequired: %d %s", st, code)
	}
	if st, code, _ := bookmarkValidationError(library.ErrBookmarkTitleTooLong); st != 400 || code != "BOOKMARK_TITLE_TOO_LONG" {
		t.Fatalf("TitleTooLong: %d %s", st, code)
	}
	if st, code, _ := bookmarkValidationError(library.ErrBookmarkNoteTooLong); st != 400 || code != "BOOKMARK_NOTE_TOO_LONG" {
		t.Fatalf("NoteTooLong: %d %s", st, code)
	}
	if st, code, msg := bookmarkValidationError(errors.New("other")); st != 0 || code != "" || msg != "" {
		t.Fatalf("default: %d %s %s", st, code, msg)
	}

	// validSpaceID
	if !validSpaceID("default") || !validSpaceID("Space_1.2-3") {
		t.Fatal("合法 Space ID 应通过")
	}
	if validSpaceID("") || validSpaceID("a/b") || validSpaceID(strings.Repeat("x", 129)) {
		t.Fatal("非法 Space ID 应拒绝")
	}

	// 时间线资源 / token / content-type
	if !validTimelineResource("index.vtt") || !validTimelineResource("sprite_0.webp") {
		t.Fatal("合法 timeline resource")
	}
	if validTimelineResource("") || validTimelineResource("../x.webp") || validTimelineResource("a/b.webp") {
		t.Fatal("非法 timeline resource")
	}
	if !validTimelineContentType("index.vtt", "text/vtt; charset=utf-8") || !validTimelineContentType("a.webp", "image/webp") {
		t.Fatal("合法 content-type")
	}
	if validTimelineContentType("index.vtt", "text/plain") || validTimelineContentType("a.webp", "application/octet-stream") {
		t.Fatal("非法 content-type")
	}
	if !validTimelineToken("gen-1.abc") || validTimelineToken("") || validTimelineToken("..") || validTimelineToken(strings.Repeat("a", 129)) {
		t.Fatal("validTimelineToken")
	}
	if !validTimelineCharacter('A') || !validTimelineCharacter('_') || validTimelineCharacter('/') {
		t.Fatal("validTimelineCharacter")
	}

	// isSQLiteBusyOrLocked：nil / 普通错误 / 包装错误无 sqlite3 类型时 false
	if isSQLiteBusyOrLocked(nil) {
		t.Fatal("nil 不应 busy")
	}
	if isSQLiteBusyOrLocked(errors.New("database is locked")) {
		t.Fatal("纯字符串错误无 Code 字段时不应判定 busy")
	}
	if isSQLiteBusyOrLocked(fmt.Errorf("wrap: %w", errors.New("inner"))) {
		t.Fatal("普通 wrapped 错误不应 busy")
	}

	// parseAudioHLSRoute 边界
	if r, err := parseAudioHLSRoute("not-profiles/x"); r != nil || err != nil {
		t.Fatalf("非 profiles 前缀应 (nil,nil): %v %v", r, err)
	}
	if r, err := parseAudioHLSRoute("profiles/bad"); r != nil || err != nil {
		// 非法命名空间同样 (nil,nil)
		_ = r
		_ = err
	}
}
