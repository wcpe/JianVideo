package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

const (
	frameMarkerBits      = 9
	frameMarkerCellSize  = 8
	frameMarkerX         = 16
	frameMarkerY         = 16
	frameMarkerThreshold = 160
	maxExactFrameCount   = 1 << frameMarkerBits
	frameProbeTimeout    = 12 * time.Second
)

var detectFramePresentation = probeFramePresentation

type frameProbeOutput struct {
	Streams []struct {
		AverageFrameRate string `json:"avg_frame_rate"`
		FrameCount       string `json:"nb_frames"`
		Height           int    `json:"height"`
		Width            int    `json:"width"`
	} `json:"streams"`
}

type frameProbeMetadata struct {
	frameCount int
	frameRate  float64
	height     int
	width      int
}

func probeFramePresentation(ctx context.Context, path string) *transcoder.FramePresentationDescriptor {
	metadata, ok := probeFrameMetadata(ctx, path)
	if !ok || !markerFits(metadata) {
		return nil
	}
	indices := verificationFrameIndices(metadata.frameCount)
	frames, ok := extractMarkerFrames(ctx, path, indices)
	if !ok || !markerFramesMatch(frames, indices) {
		return nil
	}
	return buildFramePresentation(metadata.frameRate, metadata.frameCount)
}

func probeFrameMetadata(ctx context.Context, path string) (frameProbeMetadata, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, frameProbeTimeout)
	defer cancel()
	args := []string{
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=avg_frame_rate,nb_frames,width,height",
		"-of", "json", filepath.FromSlash(path),
	}
	output, err := exec.CommandContext(probeCtx, transcoder.GetFFprobePath(), args...).Output()
	if err != nil {
		return frameProbeMetadata{}, false
	}
	return parseFrameProbe(output)
}

func parseFrameProbe(raw []byte) (frameProbeMetadata, bool) {
	var output frameProbeOutput
	if err := json.Unmarshal(raw, &output); err != nil || len(output.Streams) == 0 {
		return frameProbeMetadata{}, false
	}
	stream := output.Streams[0]
	frameRate, rateOK := parseFrameRate(stream.AverageFrameRate)
	frameCount, countErr := strconv.Atoi(strings.TrimSpace(stream.FrameCount))
	if !rateOK || countErr != nil || frameCount < 1 || frameCount > maxExactFrameCount {
		return frameProbeMetadata{}, false
	}
	return frameProbeMetadata{frameCount: frameCount, frameRate: frameRate, height: stream.Height, width: stream.Width}, true
}

func parseFrameRate(raw string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 2 {
		return 0, false
	}
	numerator, numeratorErr := strconv.ParseFloat(parts[0], 64)
	denominator, denominatorErr := strconv.ParseFloat(parts[1], 64)
	if numeratorErr != nil || denominatorErr != nil || numerator <= 0 || denominator <= 0 {
		return 0, false
	}
	return numerator / denominator, true
}

func markerFits(metadata frameProbeMetadata) bool {
	markerWidth := (frameMarkerBits + 2) * frameMarkerCellSize
	return metadata.width >= frameMarkerX+markerWidth && metadata.height >= frameMarkerY+frameMarkerCellSize
}

func verificationFrameIndices(frameCount int) []int {
	candidates := []int{0, frameCount / 2, frameCount - 1}
	indices := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if len(indices) == 0 || indices[len(indices)-1] != candidate {
			indices = append(indices, candidate)
		}
	}
	return indices
}

func extractMarkerFrames(ctx context.Context, path string, indices []int) ([][]byte, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, frameProbeTimeout)
	defer cancel()
	filter := markerSelectFilter(indices)
	args := []string{
		"-v", "error", "-i", filepath.FromSlash(path), "-map", "0:v:0", "-an",
		"-vf", filter, "-vsync", "0", "-f", "rawvideo", "-pix_fmt", "gray", "-",
	}
	output, err := exec.CommandContext(probeCtx, transcoder.GetFFmpegPath(), args...).Output()
	if err != nil {
		return nil, false
	}
	return splitMarkerFrames(output, len(indices))
}

func markerSelectFilter(indices []int) string {
	terms := make([]string, 0, len(indices))
	for _, index := range indices {
		terms = append(terms, fmt.Sprintf("eq(n\\,%d)", index))
	}
	width := (frameMarkerBits + 2) * frameMarkerCellSize
	return fmt.Sprintf("select=%s,crop=%d:%d:%d:%d,format=gray", strings.Join(terms, "+"), width, frameMarkerCellSize, frameMarkerX, frameMarkerY)
}

func splitMarkerFrames(raw []byte, count int) ([][]byte, bool) {
	width := (frameMarkerBits + 2) * frameMarkerCellSize
	frameSize := width * frameMarkerCellSize
	if len(raw) != count*frameSize {
		return nil, false
	}
	frames := make([][]byte, count)
	for index := range frames {
		start := index * frameSize
		frames[index] = raw[start : start+frameSize]
	}
	return frames, true
}

func markerFramesMatch(frames [][]byte, indices []int) bool {
	if len(frames) != len(indices) {
		return false
	}
	for index, frame := range frames {
		decoded, ok := decodeMarkerFrame(frame)
		if !ok || decoded != indices[index] {
			return false
		}
	}
	return true
}

func decodeMarkerFrame(frame []byte) (int, bool) {
	if !markerCellWhite(frame, 0) || !markerCellWhite(frame, frameMarkerBits+1) {
		return 0, false
	}
	decoded := 0
	for bit := 0; bit < frameMarkerBits; bit++ {
		if markerCellWhite(frame, bit+1) {
			decoded += 1 << bit
		}
	}
	return decoded, true
}

func markerCellWhite(frame []byte, cell int) bool {
	width := (frameMarkerBits + 2) * frameMarkerCellSize
	centerX := cell*frameMarkerCellSize + frameMarkerCellSize/2
	centerY := frameMarkerCellSize / 2
	total := 0
	for y := centerY - 1; y <= centerY+1; y++ {
		for x := centerX - 1; x <= centerX+1; x++ {
			total += int(frame[y*width+x])
		}
	}
	return total/9 >= frameMarkerThreshold
}

func buildFramePresentation(frameRate float64, frameCount int) *transcoder.FramePresentationDescriptor {
	timeline := make([]transcoder.FrameTimelineEntry, frameCount)
	for index := range timeline {
		timeline[index] = transcoder.FrameTimelineEntry{
			MediaTime: indexToMediaTime(index, frameRate), SourceFrameIndex: index,
			StableFrameID: fmt.Sprintf("binary-marker:%d", index),
		}
	}
	return &transcoder.FramePresentationDescriptor{
		Marker: transcoder.FrameMarkerDescriptor{
			Bits: frameMarkerBits, CellSize: frameMarkerCellSize,
			Threshold: frameMarkerThreshold, X: frameMarkerX, Y: frameMarkerY,
		},
		NominalFrameRate: frameRate,
		Timeline:         timeline,
	}
}

func indexToMediaTime(index int, frameRate float64) float64 {
	return (float64(index) + 0.5) / frameRate
}
