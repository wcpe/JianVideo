package library

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// MediaCursor 是媒体列表游标，按排序时间与 ID 固定下一页边界。
type MediaCursor struct {
	SortTime time.Time `json:"sort_time"`
	ID       int64     `json:"id"`
}

// EncodeMediaCursor 将游标编码为 URL 安全 token。
func EncodeMediaCursor(cursor MediaCursor) (string, error) {
	if cursor.SortTime.IsZero() || cursor.ID <= 0 {
		return "", fmt.Errorf("cursor 边界无效")
	}
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeMediaCursor 解码 URL 安全 cursor token。
func DecodeMediaCursor(token string) (MediaCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return MediaCursor{}, fmt.Errorf("cursor 格式无效")
	}
	var cursor MediaCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return MediaCursor{}, fmt.Errorf("cursor 内容无效")
	}
	if cursor.SortTime.IsZero() || cursor.ID <= 0 {
		return MediaCursor{}, fmt.Errorf("cursor 边界无效")
	}
	return cursor, nil
}
