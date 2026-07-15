package api

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"
)

func TestParseFrameProbe_AcceptsBoundedConstantRateVideo(t *testing.T) {
	raw := frameProbeJSON(t, "2/1", 260, func(index int) float64 {
		return float64(index) / 2
	})

	metadata, ok := parseFrameProbe(raw)

	if !ok {
		t.Fatal("有界恒定帧率视频应通过元数据校验")
	}
	if metadata.frameRate != 2 || metadata.frameCount != 260 || metadata.frameTimes[259] != 129.5 {
		t.Fatalf("元数据不符: %+v", metadata)
	}
}

func TestParseFrameProbe_RejectsUnboundedOrUnknownFrameCount(t *testing.T) {
	for _, count := range []string{"N/A", "513"} {
		t.Run(count, func(t *testing.T) {
			raw := []byte(`{"streams":[{"avg_frame_rate":"30/1","nb_frames":"` + count + `","width":320,"height":180}]}`)
			if _, ok := parseFrameProbe(raw); ok {
				t.Fatalf("帧数 %q 不得生成 exact 契约", count)
			}
		})
	}
}

func TestParseFrameProbe_KeepsVariableFrameTimestamps(t *testing.T) {
	times := []float64{0, 0.04, 0.11}
	raw := frameProbeJSON(t, "25/1", len(times), func(index int) float64 {
		return times[index]
	})

	metadata, ok := parseFrameProbe(raw)

	if !ok {
		t.Fatal("具有完整真实时间轴的可变帧率视频应通过")
	}
	descriptor := buildFramePresentationFromTimes(metadata.frameRate, metadata.frameTimes)
	if math.Abs(descriptor.Timeline[1].MediaTime-0.04) > 1e-9 ||
		math.Abs(descriptor.Timeline[2].MediaTime-0.11) > 1e-9 {
		t.Fatalf("应保留真实逐帧时间戳: %+v", descriptor.Timeline)
	}
}

func TestIsExactFrameSource_RejectsNonH264Original(t *testing.T) {
	metadata := frameProbeMetadata{codecName: "hevc", width: 320, height: 180}
	if isExactFrameSource(metadata) {
		t.Fatal("非 H.264 原文件不得直出并宣称精确逐帧")
	}
	metadata.codecName = "h264"
	if !isExactFrameSource(metadata) {
		t.Fatal("满足 marker 尺寸的 H.264 原文件应允许继续验证")
	}
}

func TestMarkerFramesMatch_RequiresPixelIdentityEqualFrameIndex(t *testing.T) {
	indices := []int{0, 1, 2}
	frames := make([][]byte, len(indices))
	for index, frameIndex := range indices {
		frames[index] = markerFrame(frameIndex, true)
	}

	if !markerFramesMatchAll(frames) {
		t.Fatal("真实 marker 身份与帧索引一致时应通过验证")
	}
	frames[1] = markerFrame(129, true)
	if markerFramesMatchAll(frames) {
		t.Fatal("任一帧身份不匹配时不得生成 exact 契约")
	}
	frames[1] = markerFrame(1, false)
	if markerFramesMatchAll(frames) {
		t.Fatal("marker 哨兵缺失时不得生成 exact 契约")
	}
}

func TestFrameProbeSlots_RejectsBeyondGlobalLimit(t *testing.T) {
	acquired := 0
	defer func() {
		for acquired > 0 {
			releaseFrameProbeSlot()
			acquired--
		}
	}()
	for acquired < maxConcurrentFrameProbes {
		if !acquireFrameProbeSlot() {
			t.Fatalf("第 %d 个探测槽位应可用", acquired+1)
		}
		acquired++
	}
	if acquireFrameProbeSlot() {
		t.Fatal("超过全局探测并发上限时必须快速降级")
	}
}

func TestFramePresentationCache_MergesSameFingerprint(t *testing.T) {
	cache := newFramePresentationCache()
	key := frameProbeCacheKey{path: "video.mp4", size: 10, modTime: 20}
	entry, owner := cache.loadOrCreate(key)
	if !owner {
		t.Fatal("首个请求应负责执行探测")
	}
	shared, secondOwner := cache.loadOrCreate(key)
	if secondOwner || shared != entry {
		t.Fatal("同一文件指纹应合并到同一探测")
	}
	descriptor := buildFramePresentation(2, 2)
	cache.complete(key, entry, descriptor, true)
	if waitForFrameProbe(context.Background(), shared) != descriptor {
		t.Fatal("并发等待者应复用已完成的探测结果")
	}
}

func TestFramePresentationCache_TransientFailureAllowsRetry(t *testing.T) {
	cache := newFramePresentationCache()
	key := frameProbeCacheKey{path: "retry.mp4", size: 10, modTime: 20}
	entry, owner := cache.loadOrCreate(key)
	if !owner {
		t.Fatal("首个请求应负责执行探测")
	}
	cache.complete(key, entry, nil, false)

	retry, retryOwner := cache.loadOrCreate(key)
	if !retryOwner || retry == entry {
		t.Fatal("临时失败后同一文件必须允许重新探测")
	}
}

func TestBuildFramePresentation_BoundsTimelineAndKeepsStableIndex(t *testing.T) {
	descriptor := buildFramePresentation(2, 260)

	if len(descriptor.Timeline) != 260 || len(descriptor.Timeline) > maxExactFrameCount {
		t.Fatalf("时间线必须有界且覆盖验证帧，实得 %d", len(descriptor.Timeline))
	}
	last := descriptor.Timeline[259]
	if last.MediaTime != 129.5 || last.SourceFrameIndex != 259 || last.StableFrameID != "binary-marker:259" {
		t.Fatalf("末帧索引契约不符: %+v", last)
	}
}

func frameProbeJSON(t *testing.T, frameRate string, count int, timestamp func(int) float64) []byte {
	t.Helper()
	frames := make([]map[string]string, count)
	for index := range frames {
		frames[index] = map[string]string{
			"best_effort_timestamp_time": strconv.FormatFloat(timestamp(index), 'f', -1, 64),
		}
	}
	raw, err := json.Marshal(map[string]any{
		"streams": []map[string]any{{
			"codec_name": "h264", "avg_frame_rate": frameRate, "nb_frames": strconv.Itoa(count),
			"width": 320, "height": 180,
		}},
		"frames": frames,
	})
	if err != nil {
		t.Fatalf("构造 ffprobe 数据失败: %v", err)
	}
	return raw
}

func markerFrame(index int, validSentinels bool) []byte {
	width := (frameMarkerBits + 2) * frameMarkerCellSize
	frame := make([]byte, width*frameMarkerCellSize)
	for cell := 0; cell < frameMarkerBits+2; cell++ {
		sentinel := cell == 0 || cell == frameMarkerBits+1
		white := sentinel && validSentinels
		if !sentinel {
			white = index&(1<<(cell-1)) != 0
		}
		if white {
			fillMarkerCell(frame, width, cell)
		}
	}
	return frame
}

func fillMarkerCell(frame []byte, width, cell int) {
	for y := 0; y < frameMarkerCellSize; y++ {
		for x := cell * frameMarkerCellSize; x < (cell+1)*frameMarkerCellSize; x++ {
			frame[y*width+x] = 255
		}
	}
}
