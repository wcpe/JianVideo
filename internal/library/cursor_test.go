package library

import (
	"testing"
	"time"
)

func TestMediaCursorEncodeDecode(t *testing.T) {
	original := MediaCursor{
		SortTime: time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC),
		ID:       42,
	}

	token, err := EncodeMediaCursor(original)
	if err != nil {
		t.Fatalf("编码 cursor 失败: %v", err)
	}
	got, err := DecodeMediaCursor(token)
	if err != nil {
		t.Fatalf("解码 cursor 失败: %v", err)
	}

	if !got.SortTime.Equal(original.SortTime) || got.ID != original.ID {
		t.Fatalf("cursor 往返不一致: got=%+v want=%+v", got, original)
	}
}

func TestMediaCursorDecodeRejectsInvalidToken(t *testing.T) {
	if _, err := DecodeMediaCursor("not-a-cursor"); err == nil {
		t.Fatal("非法 cursor 应返回错误")
	}
}
