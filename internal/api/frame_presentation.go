package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

const (
	frameMarkerBits          = 9
	frameMarkerCellSize      = 8
	frameMarkerX             = 16
	frameMarkerY             = 16
	frameMarkerThreshold     = 160
	maxExactFrameCount       = 1 << frameMarkerBits
	maxFrameProbeCacheItems  = 256
	maxConcurrentFrameProbes = 4
	frameProbeTotalTimeout   = 12 * time.Second
)

var (
	detectFramePresentation = cachedFramePresentation
	frameProbeCache         = newFramePresentationCache()
	frameProbeSlots         = make(chan struct{}, maxConcurrentFrameProbes)
)

type frameProbeOutput struct {
	Streams []struct {
		AverageFrameRate string `json:"avg_frame_rate"`
		CodecName        string `json:"codec_name"`
		FrameCount       string `json:"nb_frames"`
		Height           int    `json:"height"`
		Width            int    `json:"width"`
	} `json:"streams"`
	Frames []struct {
		Timestamp string `json:"best_effort_timestamp_time"`
	} `json:"frames"`
}

type frameProbeMetadata struct {
	codecName  string
	frameCount int
	frameRate  float64
	frameTimes []float64
	height     int
	width      int
}

type frameProbeCacheKey struct {
	path    string
	size    int64
	modTime int64
}

type frameProbeCacheEntry struct {
	descriptor *transcoder.FramePresentationDescriptor
	ready      chan struct{}
}

type framePresentationCache struct {
	entries map[frameProbeCacheKey]*frameProbeCacheEntry
	mu      sync.Mutex
}

func newFramePresentationCache() *framePresentationCache {
	return &framePresentationCache{entries: make(map[frameProbeCacheKey]*frameProbeCacheEntry)}
}

func cachedFramePresentation(ctx context.Context, path string) *transcoder.FramePresentationDescriptor {
	key, ok := frameProbeFingerprint(path)
	if !ok {
		return nil
	}
	entry, owner := frameProbeCache.loadOrCreate(key)
	if !owner {
		return waitForFrameProbe(ctx, entry)
	}
	if !acquireFrameProbeSlot() {
		frameProbeCache.complete(key, entry, nil, false)
		return nil
	}
	defer releaseFrameProbeSlot()
	probeCtx, cancel := context.WithTimeout(ctx, frameProbeTotalTimeout)
	defer cancel()
	descriptor, cacheable := probeFramePresentation(probeCtx, path)
	frameProbeCache.complete(key, entry, descriptor, cacheable && ctx.Err() == nil)
	return descriptor
}

func acquireFrameProbeSlot() bool {
	select {
	case frameProbeSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func releaseFrameProbeSlot() {
	<-frameProbeSlots
}

func frameProbeFingerprint(path string) (frameProbeCacheKey, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return frameProbeCacheKey{}, false
	}
	return frameProbeCacheKey{
		path: filepath.Clean(path), size: info.Size(), modTime: info.ModTime().UnixNano(),
	}, true
}

func (cache *framePresentationCache) loadOrCreate(key frameProbeCacheKey) (*frameProbeCacheEntry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if entry := cache.entries[key]; entry != nil {
		return entry, false
	}
	cache.trimCompleted()
	entry := &frameProbeCacheEntry{ready: make(chan struct{})}
	cache.entries[key] = entry
	return entry, true
}

func (cache *framePresentationCache) complete(
	key frameProbeCacheKey,
	entry *frameProbeCacheEntry,
	descriptor *transcoder.FramePresentationDescriptor,
	cacheable bool,
) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cacheable {
		entry.descriptor = descriptor
	} else {
		delete(cache.entries, key)
	}
	close(entry.ready)
}

func (cache *framePresentationCache) trimCompleted() {
	if len(cache.entries) < maxFrameProbeCacheItems {
		return
	}
	for key, entry := range cache.entries {
		select {
		case <-entry.ready:
			delete(cache.entries, key)
			return
		default:
		}
	}
}

func waitForFrameProbe(ctx context.Context, entry *frameProbeCacheEntry) *transcoder.FramePresentationDescriptor {
	select {
	case <-entry.ready:
		return entry.descriptor
	case <-ctx.Done():
		return nil
	}
}

func probeFramePresentation(
	ctx context.Context,
	path string,
) (*transcoder.FramePresentationDescriptor, bool) {
	metadata, supported, cacheable := probeFrameMetadata(ctx, path)
	if !supported || !isExactFrameSource(metadata) {
		return nil, cacheable
	}
	frames, extracted := extractMarkerFrames(ctx, path, metadata.frameCount)
	if !extracted {
		return nil, false
	}
	if !markerFramesMatchAll(frames) {
		return nil, true
	}
	return buildFramePresentationFromTimes(metadata.frameRate, metadata.frameTimes), true
}

func probeFrameMetadata(ctx context.Context, path string) (frameProbeMetadata, bool, bool) {
	args := []string{
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,avg_frame_rate,nb_frames,width,height:frame=best_effort_timestamp_time",
		"-of", "json", filepath.FromSlash(path),
	}
	output, err := exec.CommandContext(ctx, transcoder.GetFFprobePath(), args...).Output()
	if err != nil {
		return frameProbeMetadata{}, false, false
	}
	metadata, ok := parseFrameProbe(output)
	return metadata, ok, true
}

func parseFrameProbe(raw []byte) (frameProbeMetadata, bool) {
	var output frameProbeOutput
	if err := json.Unmarshal(raw, &output); err != nil || len(output.Streams) == 0 {
		return frameProbeMetadata{}, false
	}
	stream := output.Streams[0]
	frameRate, rateOK := parseFrameRate(stream.AverageFrameRate)
	frameCount, countErr := strconv.Atoi(strings.TrimSpace(stream.FrameCount))
	frameTimes, timesOK := parseFrameTimes(output.Frames, frameCount)
	if !rateOK || countErr != nil || !timesOK || frameCount < 1 || frameCount > maxExactFrameCount {
		return frameProbeMetadata{}, false
	}
	codecName := strings.ToLower(strings.TrimSpace(stream.CodecName))
	if codecName == "" {
		return frameProbeMetadata{}, false
	}
	return frameProbeMetadata{
		codecName: codecName, frameCount: frameCount, frameRate: frameRate,
		frameTimes: frameTimes, height: stream.Height, width: stream.Width,
	}, true
}

func parseFrameTimes(frames []struct {
	Timestamp string `json:"best_effort_timestamp_time"`
}, expected int) ([]float64, bool) {
	if expected < 1 || len(frames) != expected {
		return nil, false
	}
	times := make([]float64, expected)
	for index, frame := range frames {
		value, err := strconv.ParseFloat(strings.TrimSpace(frame.Timestamp), 64)
		if err != nil || value < 0 || (index > 0 && value <= times[index-1]) {
			return nil, false
		}
		times[index] = value
	}
	return times, true
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

func isExactFrameSource(metadata frameProbeMetadata) bool {
	return metadata.codecName == "h264" && markerFits(metadata)
}

func markerFits(metadata frameProbeMetadata) bool {
	markerWidth := (frameMarkerBits + 2) * frameMarkerCellSize
	return metadata.width >= frameMarkerX+markerWidth && metadata.height >= frameMarkerY+frameMarkerCellSize
}

func extractMarkerFrames(ctx context.Context, path string, frameCount int) ([][]byte, bool) {
	width := (frameMarkerBits + 2) * frameMarkerCellSize
	filter := fmt.Sprintf("crop=%d:%d:%d:%d,format=gray", width, frameMarkerCellSize, frameMarkerX, frameMarkerY)
	args := []string{
		"-v", "error", "-i", filepath.FromSlash(path), "-map", "0:v:0", "-an",
		"-vf", filter, "-vsync", "0", "-f", "rawvideo", "-pix_fmt", "gray", "-",
	}
	output, err := exec.CommandContext(ctx, transcoder.GetFFmpegPath(), args...).Output()
	if err != nil {
		return nil, false
	}
	return splitMarkerFrames(output, frameCount)
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

func markerFramesMatchAll(frames [][]byte) bool {
	for index, frame := range frames {
		decoded, ok := decodeMarkerFrame(frame)
		if !ok || decoded != index {
			return false
		}
	}
	return len(frames) > 0
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
	frameTimes := make([]float64, frameCount)
	for index := range frameTimes {
		frameTimes[index] = float64(index) / frameRate
	}
	return buildFramePresentationFromTimes(frameRate, frameTimes)
}

func buildFramePresentationFromTimes(
	frameRate float64,
	frameTimes []float64,
) *transcoder.FramePresentationDescriptor {
	timeline := make([]transcoder.FrameTimelineEntry, len(frameTimes))
	for index := range timeline {
		timeline[index] = transcoder.FrameTimelineEntry{
			MediaTime: frameTimes[index], SourceFrameIndex: index,
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
