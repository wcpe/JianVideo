package transcoder

import (
	"reflect"
	"testing"
)

// TestChosenCodec 穷举编码协商纯函数的各分支（FR-53 核心）。
func TestChosenCodec(t *testing.T) {
	cases := []struct {
		name       string
		priority   []string
		clientCaps map[string]bool
		producible map[string]bool
		want       string
	}{
		{
			name:       "首选命中：av1 客户端支持且可产出",
			priority:   []string{"av1", "h265", "h264"},
			clientCaps: map[string]bool{"av1": true, "h265": true},
			producible: map[string]bool{"av1": true, "h265": true, "h264": true},
			want:       "av1",
		},
		{
			name:       "客户端不支持跳过：av1 不被客户端支持，落到 h265",
			priority:   []string{"av1", "h265", "h264"},
			clientCaps: map[string]bool{"av1": false, "h265": true},
			producible: map[string]bool{"av1": true, "h265": true, "h264": true},
			want:       "h265",
		},
		{
			name:       "不可产出跳过：av1 客户端支持但系统不可产出，落到 h265",
			priority:   []string{"av1", "h265", "h264"},
			clientCaps: map[string]bool{"av1": true, "h265": true},
			producible: map[string]bool{"av1": false, "h265": true, "h264": true},
			want:       "h265",
		},
		{
			name:       "全不满足兜底 h264：高级编码均不可用",
			priority:   []string{"av1", "h265"},
			clientCaps: map[string]bool{"av1": false, "h265": false},
			producible: map[string]bool{"av1": true, "h265": true},
			want:       "h264",
		},
		{
			name:       "空优先级兜底 h264",
			priority:   nil,
			clientCaps: map[string]bool{"av1": true},
			producible: map[string]bool{"av1": true},
			want:       "h264",
		},
		{
			name:       "优先级显式含 h264 且高级编码不可用：返回 h264",
			priority:   []string{"av1", "h264"},
			clientCaps: map[string]bool{"av1": false},
			producible: map[string]bool{"av1": false, "h264": true},
			want:       "h264",
		},
		{
			name:       "归一化：HEVC 大写 + hevc 别名视为 h265",
			priority:   []string{"HEVC"},
			clientCaps: map[string]bool{"h265": true},
			producible: map[string]bool{"h265": true},
			want:       "h265",
		},
		{
			name:       "首个不可产出、次选可用：vp9 命中",
			priority:   []string{"av1", "vp9", "h264"},
			clientCaps: map[string]bool{"av1": true, "vp9": true},
			producible: map[string]bool{"av1": false, "vp9": true, "h264": true},
			want:       "vp9",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ChosenCodec(tc.priority, tc.clientCaps, tc.producible)
			if got != tc.want {
				t.Errorf("ChosenCodec(%v, %v, %v) = %q, 期望 %q",
					tc.priority, tc.clientCaps, tc.producible, got, tc.want)
			}
		})
	}
}

// TestBuildNegotiationDescriptor_TS h264 协商结果映射为 TS 描述符。
func TestBuildNegotiationDescriptor_TS(t *testing.T) {
	d := BuildNegotiationDescriptor(42, "h264")
	if d.Codec != "h264" {
		t.Errorf("codec 期望 h264，实得 %q", d.Codec)
	}
	if d.Path != "ts" {
		t.Errorf("path 期望 ts，实得 %q", d.Path)
	}
	if d.URL != "/api/play/hls/42/master" {
		t.Errorf("url 期望 /api/play/hls/42/master，实得 %q", d.URL)
	}
	if d.MIME != "" {
		t.Errorf("ts 路径 mime 应为空，实得 %q", d.MIME)
	}
	if d.FallbackURL != "" {
		t.Errorf("ts 路径无需 fallback，实得 %q", d.FallbackURL)
	}
}

// TestBuildNegotiationDescriptor_FMP4 高级编码协商结果映射为 fMP4 描述符。
func TestBuildNegotiationDescriptor_FMP4(t *testing.T) {
	d := BuildNegotiationDescriptor(7, "av1")
	if d.Codec != "av1" {
		t.Errorf("codec 期望 av1，实得 %q", d.Codec)
	}
	if d.Path != "fmp4" {
		t.Errorf("path 期望 fmp4，实得 %q", d.Path)
	}
	if d.URL != "/api/play/hls/7/index.m3u8" {
		t.Errorf("url 期望 /api/play/hls/7/index.m3u8，实得 %q", d.URL)
	}
	if d.MIME != FMP4CodecMIME("av1") {
		t.Errorf("mime 期望 %q，实得 %q", FMP4CodecMIME("av1"), d.MIME)
	}
	if d.FallbackURL != "/api/play/7/stream" {
		t.Errorf("fmp4 路径需 H.264 回退源 /api/play/7/stream，实得 %q", d.FallbackURL)
	}
}

// TestBuildNegotiationDescriptor_Normalize hevc 归一化为 h265 后走 fMP4。
func TestBuildNegotiationDescriptor_Normalize(t *testing.T) {
	d := BuildNegotiationDescriptor(1, "HEVC")
	want := NegotiationDescriptor{
		Codec:       "h265",
		Path:        "fmp4",
		URL:         "/api/play/hls/1/index.m3u8",
		MIME:        FMP4CodecMIME("h265"),
		FallbackURL: "/api/play/1/stream",
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("描述符不符\n实得 %+v\n期望 %+v", d, want)
	}
}
