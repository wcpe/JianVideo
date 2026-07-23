package models

import "testing"

func TestContentVisibleAndRank(t *testing.T) {
	if !ContentVisible("R", "") {
		t.Fatal("无上限应全可见")
	}
	if ContentVisible("R", "PG") {
		t.Fatal("PG 不应看到 R")
	}
	if !ContentVisible("PG", "PG-13") {
		t.Fatal("PG-13 应看到 PG")
	}
	if !ContentVisible("UNRATED", "G") {
		t.Fatal("UNRATED 默认可见")
	}
	if !ContentVisible("", "G") {
		t.Fatal("空分级默认可见")
	}
	if ContentRatingRank("PG-13") != ContentRatingRank("PG13") {
		t.Fatal("PG13 应与 PG-13 同级")
	}
}

func TestContentRatingsAtMost(t *testing.T) {
	got := ContentRatingsAtMost("PG")
	if len(got) != 2 || got[0] != "G" || got[1] != "PG" {
		t.Fatalf("PG 上限期望 [G PG], 实际 %v", got)
	}
}
