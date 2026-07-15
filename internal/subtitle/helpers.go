package subtitle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

var imageSubtitleCodecs = map[string]bool{
	"dvb_subtitle": true, "dvd_subtitle": true, "hdmv_pgs_subtitle": true, "xsub": true,
}

var textSubtitleCodecs = map[string]string{
	"ass": "ass", "ssa": "ssa", "subrip": "srt", "srt": "srt", "webvtt": "vtt",
	"mov_text": "srt", "text": "srt", "microdvd": "srt", "sami": "srt", "realtext": "srt",
}

type auditRecorder interface {
	Record(context.Context, audit.EventInput) error
	RecordTx(context.Context, *gorm.DB, audit.EventInput) error
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func safeSegment(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func subtitleCodecCapability(codec string) (bool, string) {
	codec = strings.ToLower(codec)
	if imageSubtitleCodecs[codec] {
		return false, ReasonImageSubtitleUnsupported
	}
	if _, ok := textSubtitleCodecs[codec]; ok {
		return true, ""
	}
	return false, ReasonSubtitleCodecUnsupported
}

func subtitleFormat(codec string) string {
	return textSubtitleCodecs[strings.ToLower(codec)]
}

func trackLabel(stream streamMetadata, fallback string) string {
	parts := make([]string, 0, 2)
	if stream.Language != "" {
		parts = append(parts, stream.Language)
	}
	if stream.Title != "" {
		parts = append(parts, stream.Title)
	}
	if len(parts) == 0 {
		return fallback
	}
	return strings.Join(parts, " · ")
}

func normalizedSidecarRef(mediaPath, subtitlePath string) string {
	relative, err := filepath.Rel(filepath.Dir(mediaPath), subtitlePath)
	if err != nil {
		return strings.ToLower(filepath.Base(subtitlePath))
	}
	return strings.ToLower(filepath.ToSlash(relative))
}

func sidecarTitleLanguage(mediaPath, subtitlePath string) (string, string) {
	mediaBase := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	subtitleBase := strings.TrimSuffix(filepath.Base(subtitlePath), filepath.Ext(subtitlePath))
	suffix := strings.TrimPrefix(subtitleBase[len(mediaBase):], ".")
	if suffix == "" {
		return subtitleBase, ""
	}
	parts := strings.Split(suffix, ".")
	return subtitleBase, parts[0]
}

func extractEmbeddedSubtitle(ctx context.Context, mediaPath string, streamIndex int, outputPath string) error {
	args := []string{"-v", "error", "-y", "-i", filepath.FromSlash(mediaPath), "-map", fmt.Sprintf("0:%d", streamIndex), outputPath}
	// #nosec G204 -- ffmpeg 路径来自受控配置，参数按固定模板构造并直接传递，不经过 shell。
	output, err := exec.CommandContext(ctx, transcoder.GetFFmpegPath(), args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 提取字幕失败: %w; %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func containedPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || relative == "" {
		return "", ErrInvalid
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(cleanRoot, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cleanRoot, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalid
	}
	return candidate, nil
}

func requestTempFile(dir, pattern string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, pattern)
}
