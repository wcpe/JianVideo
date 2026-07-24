package models

import (
	"testing"
	"time"
)

func TestRoleRankAndAtLeast(t *testing.T) {
	if RoleRank(SpaceRoleOwner) != 3 || RoleRank(SpaceRoleEditor) != 2 || RoleRank(SpaceRoleViewer) != 1 {
		t.Fatalf("角色权重异常 owner=%d editor=%d viewer=%d", RoleRank(SpaceRoleOwner), RoleRank(SpaceRoleEditor), RoleRank(SpaceRoleViewer))
	}
	if RoleRank("unknown") != 0 {
		t.Fatal("未知角色应为 0")
	}
	if !RoleAtLeast(SpaceRoleOwner, SpaceRoleEditor) {
		t.Fatal("owner 应不低于 editor")
	}
	if RoleAtLeast(SpaceRoleViewer, SpaceRoleEditor) {
		t.Fatal("viewer 不应达到 editor")
	}
	if !RoleAtLeast(SpaceRoleEditor, SpaceRoleEditor) {
		t.Fatal("同级应通过")
	}
}

func TestNormalizeAndValidContentRating(t *testing.T) {
	cases := []struct {
		in, want string
		valid    bool
	}{
		{" g ", ContentRatingG, true},
		{"pg", ContentRatingPG, true},
		{"PG13", ContentRatingPG13, true},
		{"PG-13", ContentRatingPG13, true},
		{"R", ContentRatingR, true},
		{"UNRATED", ContentRatingUnrated, true},
		{"", "", true},
		{"  ", "", true},
		{"X", "", false},
		{"NC-17", "", false},
	}
	for _, c := range cases {
		got := NormalizeContentRating(c.in)
		if got != c.want {
			t.Fatalf("Normalize(%q)=%q want %q", c.in, got, c.want)
		}
		if ValidContentRating(c.in) != c.valid {
			t.Fatalf("Valid(%q)=%v want %v", c.in, ValidContentRating(c.in), c.valid)
		}
	}
	if ContentRatingRank("R") != 4 || ContentRatingRank("g") != 1 {
		t.Fatal("ContentRatingRank 大小写归一异常")
	}
	all := ContentRatingsAtMost("R")
	if len(all) != 4 {
		t.Fatalf("R 上限应含四级，实际 %v", all)
	}
	none := ContentRatingsAtMost("")
	if len(none) != 0 {
		t.Fatalf("空上限 rank=0 应无正式分级，实际 %v", none)
	}
}

func TestParseSessionTime(t *testing.T) {
	rfc := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if parseSessionTime(rfc).IsZero() {
		t.Fatal("RFC3339 应可解析")
	}
	if parseSessionTime("2026-07-24 12:00:00").IsZero() {
		t.Fatal("SQLite datetime 应可按 UTC 解析")
	}
	if !parseSessionTime("not-a-time").IsZero() {
		t.Fatal("非法时间应返回零值")
	}
	if parseSessionTime("2026-07-24 12:00:00.123456789").IsZero() {
		t.Fatal("带纳秒的 datetime 应可解析")
	}
}

func TestSpaceMemberTableName(t *testing.T) {
	if (SpaceMember{}).TableName() != "space_members" {
		t.Fatal("SpaceMember 表名应为 space_members")
	}
	if (MetricSample{}).TableName() == "" {
		t.Fatal("MetricSample TableName 不应为空")
	}
}

func TestMediaSubtitleTrackBeforeSave(t *testing.T) {
	// 基础字段缺失
	bad := &MediaSubtitleTrack{ID: "", SpaceID: "s", MediaID: 1, Source: MediaSubtitleSourceEmbedded, Format: "srt", StreamIndex: 0}
	if err := bad.BeforeSave(nil); err == nil {
		t.Fatal("空 ID 应失败")
	}

	// 内嵌：合法
	emb := &MediaSubtitleTrack{
		ID: "emb1", SpaceID: "space-a", MediaID: 9, Source: "EMBEDDED", Format: "SRT", StreamIndex: 0,
	}
	if err := emb.BeforeSave(nil); err != nil {
		t.Fatalf("内嵌合法应通过: %v", err)
	}
	if emb.Source != MediaSubtitleSourceEmbedded || emb.Format != "srt" {
		t.Fatalf("应规范化 source/format，实际 %s/%s", emb.Source, emb.Format)
	}

	// 内嵌：StreamIndex 非法
	embBad := &MediaSubtitleTrack{
		ID: "emb2", SpaceID: "space-a", MediaID: 9, Source: MediaSubtitleSourceEmbedded, Format: "srt", StreamIndex: -1,
	}
	if err := embBad.BeforeSave(nil); err == nil {
		t.Fatal("内嵌 StreamIndex<0 应失败")
	}

	// 外挂：合法（用正斜杠避免 Linux filepath.ToSlash 不转反斜杠）
	side := &MediaSubtitleTrack{
		ID: "side1", SpaceID: "space-a", MediaID: 9, Source: MediaSubtitleSourceSidecar,
		Format: "ass", StreamIndex: -1, SourceRef: "Subs/CN.ASS",
	}
	if err := side.BeforeSave(nil); err != nil {
		t.Fatalf("外挂合法应通过: %v", err)
	}
	if side.SourceRef != "subs/cn.ass" {
		t.Fatalf("SourceRef 应 slash+小写，实际 %q", side.SourceRef)
	}

	// 外挂：空 SourceRef
	sideBad := &MediaSubtitleTrack{
		ID: "side2", SpaceID: "space-a", MediaID: 9, Source: MediaSubtitleSourceSidecar,
		Format: "ass", StreamIndex: -1, SourceRef: "  ",
	}
	if err := sideBad.BeforeSave(nil); err == nil {
		t.Fatal("外挂空 SourceRef 应失败")
	}

	// 上传：合法
	upID := "up_1"
	up := &MediaSubtitleTrack{
		ID: upID, SpaceID: "space-a", MediaID: 9, Source: MediaSubtitleSourceUploaded,
		Format: "vtt", StreamIndex: -1, SourceRef: "orig.vtt",
		StorageRelativePath: "subtitles/space-a/9/up_1.vtt",
	}
	if err := up.BeforeSave(nil); err != nil {
		t.Fatalf("上传合法应通过: %v", err)
	}

	// 上传：路径不匹配
	upBad := &MediaSubtitleTrack{
		ID: upID, SpaceID: "space-a", MediaID: 9, Source: MediaSubtitleSourceUploaded,
		Format: "vtt", StreamIndex: -1, SourceRef: "orig.vtt",
		StorageRelativePath: "wrong/path.vtt",
	}
	if err := upBad.BeforeSave(nil); err == nil {
		t.Fatal("上传路径不匹配应失败")
	}

	// 未知来源
	unk := &MediaSubtitleTrack{
		ID: "u1", SpaceID: "space-a", MediaID: 9, Source: "other", Format: "srt", StreamIndex: 0,
	}
	if err := unk.BeforeSave(nil); err == nil {
		t.Fatal("未知来源应失败")
	}

	// format 含非法字符
	fmtBad := &MediaSubtitleTrack{
		ID: "f1", SpaceID: "space-a", MediaID: 9, Source: MediaSubtitleSourceEmbedded, Format: "s.rt", StreamIndex: 0,
	}
	if err := fmtBad.BeforeSave(nil); err == nil {
		t.Fatal("非法 format token 应失败")
	}
}

func TestSafeSubtitleTokenAndNormalizeRef(t *testing.T) {
	if safeSubtitleToken("") || safeSubtitleToken(".") || safeSubtitleToken("..") || safeSubtitleToken("a/b") {
		t.Fatal("非法 token 应拒绝")
	}
	if !safeSubtitleToken("srt") || !safeSubtitleToken("Up_1-2") {
		t.Fatal("合法 token 应通过")
	}
	if normalizeSubtitleSourceRef(" A/B.SRT ") != "a/b.srt" {
		t.Fatalf("normalize 异常: %q", normalizeSubtitleSourceRef(" A/B.SRT "))
	}
}
