package api

import (
	"context"
	"errors"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

// TranscoderTimelinePreviewGateway 将转码领域契约适配为 API 契约。
type TranscoderTimelinePreviewGateway struct {
	service *transcoder.TimelinePreviewService
}

// NewTimelinePreviewGateway 创建时间轴预览 API 适配器。
func NewTimelinePreviewGateway(service *transcoder.TimelinePreviewService) *TranscoderTimelinePreviewGateway {
	return &TranscoderTimelinePreviewGateway{service: service}
}

// Status 查询当前时间轴预览状态。
func (g *TranscoderTimelinePreviewGateway) Status(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	if g == nil || g.service == nil {
		return TimelinePreviewStatus{}, ErrTimelinePreviewInvalid
	}
	status, err := g.service.Status(ctx, toTranscoderTimelineIdentity(identity))
	return fromTranscoderTimelineStatus(status), mapTimelinePreviewError(err)
}

// Enqueue 幂等创建或复用时间轴预览任务。
func (g *TranscoderTimelinePreviewGateway) Enqueue(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	if g == nil || g.service == nil {
		return TimelinePreviewStatus{}, ErrTimelinePreviewInvalid
	}
	status, err := g.service.Enqueue(ctx, toTranscoderTimelineIdentity(identity))
	return fromTranscoderTimelineStatus(status), mapTimelinePreviewError(err)
}

// Rebuild 创建新的时间轴预览 generation。
func (g *TranscoderTimelinePreviewGateway) Rebuild(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	if g == nil || g.service == nil {
		return TimelinePreviewStatus{}, ErrTimelinePreviewInvalid
	}
	status, err := g.service.Rebuild(ctx, toTranscoderTimelineIdentity(identity))
	return fromTranscoderTimelineStatus(status), mapTimelinePreviewError(err)
}

// OpenResource 打开当前指针下的受控预览资源。
func (g *TranscoderTimelinePreviewGateway) OpenResource(ctx context.Context, identity TimelinePreviewResourceIdentity) (TimelinePreviewResource, error) {
	if g == nil || g.service == nil {
		return TimelinePreviewResource{}, ErrTimelinePreviewInvalid
	}
	resource, err := g.service.OpenResource(ctx, toTranscoderResourceIdentity(identity))
	if err != nil {
		return TimelinePreviewResource{}, mapTimelinePreviewError(err)
	}
	return TimelinePreviewResource{Body: resource.Body, ContentType: resource.ContentType, Size: resource.Size}, nil
}

func toTranscoderTimelineIdentity(identity TimelinePreviewIdentity) transcoder.TimelinePreviewIdentity {
	return transcoder.TimelinePreviewIdentity{
		MediaID: identity.MediaID, ProfileID: identity.ProfileID, SpaceID: identity.SpaceID,
	}
}

func toTranscoderResourceIdentity(identity TimelinePreviewResourceIdentity) transcoder.TimelinePreviewResourceIdentity {
	return transcoder.TimelinePreviewResourceIdentity{
		TimelinePreviewIdentity: toTranscoderTimelineIdentity(identity.TimelinePreviewIdentity),
		GenerationID:            identity.GenerationID, ResourceName: identity.ResourceName,
		SourceFingerprint: identity.SourceFingerprint,
	}
}

func fromTranscoderTimelineStatus(status transcoder.TimelinePreviewStatus) TimelinePreviewStatus {
	return TimelinePreviewStatus{
		GenerationID: status.GenerationID, ProfileID: status.ProfileID,
		SourceFingerprint: status.SourceFingerprint, State: fromTranscoderTimelineState(status.State),
		Duration: status.Duration, Version: status.Version,
		SpriteNames: append([]string(nil), status.SpriteNames...), TaskID: status.TaskID,
	}
}

func fromTranscoderTimelineState(state string) string {
	switch state {
	case transcoder.TimelinePreviewAvailable:
		return TimelinePreviewAvailable
	case transcoder.TimelinePreviewPending:
		return TimelinePreviewPending
	default:
		return state
	}
}

func mapTimelinePreviewError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, transcoder.ErrTimelinePreviewInvalid):
		return errors.Join(ErrTimelinePreviewInvalid, err)
	case errors.Is(err, transcoder.ErrTimelinePreviewNotFound):
		return errors.Join(ErrTimelinePreviewNotFound, err)
	default:
		return err
	}
}
