package api

import (
	"encoding/json"
	"testing"
)

func TestParseFrameProbe_AcceptsBoundedConstantRateVideo(t *testing.T) {
	raw, _ := json.Marshal(frameProbeOutput{Streams: []struct {
		AverageFrameRate string `json:"avg_frame_rate"`
		FrameCount       string `json:"nb_frames"`
		Height           int    `json:"height"`
		Width            int    `json:"width"`
	}{{AverageFrameRate: "2/1", FrameCount: "260", Height: 180, Width: 320}}})

	metadata, ok := parseFrameProbe(raw)

	if !ok {
		t.Fatal("有界恒定帧率视频应通过元数据校验")
	}
	if metadata.frameRate != 2 || metadata.frameCount != 260 {
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

func TestMarkerFramesMatch_RequiresPixelIdentityEqualFrameIndex(t *testing.T) {
	indices := []int{0, 130, 259}
	frames := make([][]byte, len(indices))
	for index, frameIndex := range indices {
		frames[index] = markerFrame(frameIndex, true)
	}

	if !markerFramesMatch(frames, indices) {
		t.Fatal("真实 marker 身份与帧索引一致时应通过验证")
	}
	frames[1] = markerFrame(129, true)
	if markerFramesMatch(frames, indices) {
		t.Fatal("任一样本身份不匹配时不得生成 exact 契约")
	}
	frames[1] = markerFrame(130, false)
	if markerFramesMatch(frames, indices) {
		t.Fatal("marker 哨兵缺失时不得生成 exact 契约")
	}
}

func TestBuildFramePresentation_BoundsTimelineAndKeepsStableIndex(t *testing.T) {
	descriptor := buildFramePresentation(2, 260)

	if len(descriptor.Timeline) != 260 || len(descriptor.Timeline) > maxExactFrameCount {
		t.Fatalf("时间线必须有界且覆盖验证帧，实得 %d", len(descriptor.Timeline))
	}
	last := descriptor.Timeline[259]
	if last.MediaTime != 129.75 || last.SourceFrameIndex != 259 || last.StableFrameID != "binary-marker:259" {
		t.Fatalf("末帧索引契约不符: %+v", last)
	}
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
