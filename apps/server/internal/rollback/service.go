// Package rollback 提供操作可回滚中心（FR2-041）。
// 以审计事件为真源，按 action 分发 ActionReverter 执行逆操作。
package rollback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// 稳定 reason_key，便于 i18n。
const (
	ReasonNotRegistered    = "not_registered"
	ReasonMissingBefore    = "missing_before"
	ReasonSensitiveKeys    = "sensitive_keys"
	ReasonNoRevertableKeys = "no_revertable_keys"
	ReasonInvalidResource  = "invalid_resource"
	ReasonAlreadyApplied   = "already_applied"
	ReasonConfirmRequired  = "confirm_required"
	ReasonMissingSnapshot  = "missing_snapshot"
	ReasonSnapshotGone     = "snapshot_gone"
	ReasonPathRedacted     = "path_redacted"
)

// 哨兵错误。
var (
	ErrEventNotFound   = errors.New("审计事件不存在")
	ErrNotRollbackable = errors.New("事件不可回滚")
	ErrConfirmRequired = errors.New("回滚需 confirm=true")
	ErrSpaceMismatch   = errors.New("事件不属于当前 Space")
)

// ActionReverter 按 action 执行逆操作。
type ActionReverter interface {
	// CanRollback 判断事件是否可回滚；不可回滚时返回 reason_key。
	CanRollback(event *models.AuditEvent) (ok bool, reasonKey string)
	// Apply 执行回滚；成功后由 Service 写 rollback.applied 审计。
	Apply(_ context.Context, event *models.AuditEvent) error
}

// Service 回滚中心。
type Service struct {
	audit    audit.Recorder
	settings *settings.Service
	library  *library.Service
	registry map[string]ActionReverter
}

// NewService 创建回滚服务并注册首切 reverter。
func NewService(rec audit.Recorder, set *settings.Service, lib *library.Service) *Service {
	s := &Service{
		audit:    rec,
		settings: set,
		library:  lib,
		registry: map[string]ActionReverter{},
	}
	s.Register("settings.updated", &settingsUpdatedReverter{settings: set})
	s.Register("media.deleted", &mediaDeletedReverter{library: lib})
	s.Register("media.restored", &mediaRestoredReverter{library: lib})
	// 二切：写回成功 → 从审计 metadata 中的快照覆盖回原文件
	s.Register("metadata.writeback.succeeded", &writebackSucceededReverter{})
	// 二切：库内路径索引对称逆操作（磁盘改名/移动失败则 Apply 报错，写 rollback.failed）
	s.Register("media.renamed", &mediaRenamedReverter{library: lib})
	s.Register("media.moved", &mediaMovedReverter{library: lib})
	return s
}

// Register 注册 action reverter（后注册覆盖）。
func (s *Service) Register(action string, r ActionReverter) {
	if s == nil || r == nil {
		return
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return
	}
	s.registry[action] = r
}

// EventView 列表项：审计事件 + 可回滚标记。
type EventView struct {
	Event        models.AuditEvent `json:"event"`
	Rollbackable bool              `json:"rollbackable"`
	ReasonKey    string            `json:"reason_key,omitempty"`
}

// ListResult 分页结果。
type ListResult struct {
	Items      []EventView `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// ListRollbackEvents 列出近 N 天审计事件，并标注是否可回滚。
// spaceID 空表示仅 system 作用域（settings）；非空则查该 Space 的 space 事件。
// systemOnly=true 时强制 system 作用域。
func (s *Service) ListRollbackEvents(ctx context.Context, spaceID string, systemOnly bool, days int, limit int, cursor string) (ListResult, error) {
	if s == nil || s.audit == nil {
		return ListResult{}, errors.New("审计服务未启用")
	}
	if days <= 0 {
		days = 30
	}
	if limit <= 0 {
		limit = 50
	}
	from := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	q := audit.Query{
		System: systemOnly,
		From:   from,
		Cursor: cursor,
		Limit:  limit,
	}
	if !systemOnly {
		spaceID = strings.TrimSpace(spaceID)
		if spaceID == "" {
			spaceID = models.DefaultSpaceID
		}
		q.SpaceID = spaceID
	}
	page, err := s.audit.List(ctx, q)
	if err != nil {
		return ListResult{}, err
	}
	// 再拉一轮 system 事件（settings）与 space 事件合并时：首切分开查。
	// 当 systemOnly=false 时额外合并近期 system 的 settings.updated（可回滚设置）。
	items := make([]EventView, 0, len(page.Items)+8)
	for i := range page.Items {
		items = append(items, s.annotate(&page.Items[i]))
	}
	if !systemOnly {
		sysPage, err := s.audit.List(ctx, audit.Query{
			System: true,
			Action: "settings.updated",
			From:   from,
			Limit:  limit,
		})
		if err == nil {
			for i := range sysPage.Items {
				items = append(items, s.annotate(&sysPage.Items[i]))
			}
		}
	}
	return ListResult{Items: items, NextCursor: page.NextCursor}, nil
}

func (s *Service) annotate(event *models.AuditEvent) EventView {
	v := EventView{Event: *event}
	reverter, ok := s.registry[event.Action]
	if !ok {
		v.ReasonKey = ReasonNotRegistered
		return v
	}
	can, reason := reverter.CanRollback(event)
	v.Rollbackable = can
	if !can {
		v.ReasonKey = reason
	}
	return v
}

// ApplyInput 回滚请求。
type ApplyInput struct {
	EventID int64
	SpaceID string // 调用者当前 Space；system 事件可为空
	Confirm bool
	ActorID string
}

// Apply 对指定审计事件执行回滚。
func (s *Service) Apply(ctx context.Context, in ApplyInput) error {
	if s == nil || s.audit == nil {
		return errors.New("审计服务未启用")
	}
	if !in.Confirm {
		return ErrConfirmRequired
	}
	event, err := s.audit.GetByID(ctx, in.EventID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEventNotFound
		}
		return err
	}
	// Space 隔离：space 事件必须匹配；system 事件不校验 space。
	if event.Scope == audit.ScopeSpace {
		want := strings.TrimSpace(in.SpaceID)
		if want == "" {
			want = models.DefaultSpaceID
		}
		got := ""
		if event.SpaceID != nil {
			got = *event.SpaceID
		}
		if got != want {
			return ErrSpaceMismatch
		}
	}
	reverter, ok := s.registry[event.Action]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonNotRegistered)
	}
	can, reason := reverter.CanRollback(event)
	if !can {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, reason)
	}
	if err := reverter.Apply(ctx, event); err != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope:        event.Scope,
			SpaceID:      spaceIDOf(event),
			ActorType:    "user",
			ActorID:      in.ActorID,
			Action:       "rollback.failed",
			ResourceType: "audit_event",
			ResourceID:   strconv.FormatInt(event.ID, 10),
			Metadata: map[string]any{
				"original_action": event.Action,
				"error":           err.Error(),
			},
		})
		return err
	}
	return s.audit.Record(ctx, audit.EventInput{
		Scope:        event.Scope,
		SpaceID:      spaceIDOf(event),
		ActorType:    "user",
		ActorID:      in.ActorID,
		Action:       "rollback.applied",
		ResourceType: "audit_event",
		ResourceID:   strconv.FormatInt(event.ID, 10),
		Metadata: map[string]any{
			"original_action": event.Action,
			"original_id":     event.ID,
		},
	})
}

func spaceIDOf(event *models.AuditEvent) string {
	if event == nil || event.SpaceID == nil {
		return ""
	}
	return *event.SpaceID
}

// --- settings.updated ---

type settingsUpdatedReverter struct {
	settings *settings.Service
}

func (r *settingsUpdatedReverter) CanRollback(event *models.AuditEvent) (bool, string) {
	if event == nil {
		return false, ReasonMissingBefore
	}
	// 真的没有 before 字段的老事件：不可回滚（规格 §5）。
	rawBefore := strings.TrimSpace(event.BeforeJSON)
	if rawBefore == "" || rawBefore == "null" {
		return false, ReasonMissingBefore
	}
	before, err := parseStringMap(event.BeforeJSON)
	if err != nil {
		return false, ReasonMissingBefore
	}
	after, _ := parseStringMap(event.AfterJSON)
	// 敏感键：审计 before/after 均为脱敏展示值，无法还原真值 → 整事件不可回滚。
	// 首次写入时 before 可能为空 map，必须同时检查 after。
	if hasSensitiveKeys(before) || hasSensitiveKeys(after) {
		return false, ReasonSensitiveKeys
	}
	// 变更键取 before∪after；before 中缺失的键表示「此前无值」，可回滚为空串。
	if len(revertableSettingKeys(before, after)) == 0 {
		return false, ReasonNoRevertableKeys
	}
	return true, ""
}

func (r *settingsUpdatedReverter) Apply(_ context.Context, event *models.AuditEvent) error {
	if r == nil || r.settings == nil {
		return errors.New("设置服务未启用")
	}
	rawBefore := strings.TrimSpace(event.BeforeJSON)
	if rawBefore == "" || rawBefore == "null" {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonMissingBefore)
	}
	before, err := parseStringMap(event.BeforeJSON)
	if err != nil {
		return err
	}
	after, _ := parseStringMap(event.AfterJSON)
	if hasSensitiveKeys(before) || hasSensitiveKeys(after) {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonSensitiveKeys)
	}
	restore := make(map[string]string)
	for _, k := range revertableSettingKeys(before, after) {
		// before 中缺失：回滚为默认/空——SetMany 写空串清运行期值。
		if v, ok := before[k]; ok {
			restore[k] = v
		} else {
			restore[k] = ""
		}
	}
	if len(restore) == 0 {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonNoRevertableKeys)
	}
	return r.settings.SetMany(restore)
}

func hasSensitiveKeys(m map[string]string) bool {
	for k := range m {
		def, ok := settings.DefinitionByKey(k)
		if ok && def.Sensitive {
			return true
		}
	}
	return false
}

func isRuntimeNonSensitive(key string) bool {
	def, ok := settings.DefinitionByKey(key)
	if !ok {
		return false
	}
	return def.Layer == settings.LayerRuntime && !def.Sensitive
}

// revertableSettingKeys 合并 before/after 中可回滚的运行期非敏感键。
func revertableSettingKeys(maps ...map[string]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, m := range maps {
		for k := range m {
			if _, ok := seen[k]; ok {
				continue
			}
			if !isRuntimeNonSensitive(k) {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

// --- media.deleted → restore ---

type mediaDeletedReverter struct {
	library *library.Service
}

func (r *mediaDeletedReverter) CanRollback(event *models.AuditEvent) (bool, string) {
	if event == nil || strings.TrimSpace(event.ResourceID) == "" {
		return false, ReasonInvalidResource
	}
	if _, err := strconv.ParseInt(event.ResourceID, 10, 64); err != nil {
		return false, ReasonInvalidResource
	}
	// before 可选；resource_id 足够
	return true, ""
}

func (r *mediaDeletedReverter) Apply(_ context.Context, event *models.AuditEvent) error {
	if r == nil || r.library == nil {
		return errors.New("媒体库服务未启用")
	}
	id, err := strconv.ParseInt(event.ResourceID, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonInvalidResource)
	}
	spaceID := spaceIDOf(event)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	return r.library.RestoreMediaFileInSpace(spaceID, id)
}

// --- media.restored → soft-delete again ---

type mediaRestoredReverter struct {
	library *library.Service
}

func (r *mediaRestoredReverter) CanRollback(event *models.AuditEvent) (bool, string) {
	if event == nil || strings.TrimSpace(event.ResourceID) == "" {
		return false, ReasonInvalidResource
	}
	if _, err := strconv.ParseInt(event.ResourceID, 10, 64); err != nil {
		return false, ReasonInvalidResource
	}
	return true, ""
}

func (r *mediaRestoredReverter) Apply(_ context.Context, event *models.AuditEvent) error {
	if r == nil || r.library == nil {
		return errors.New("媒体库服务未启用")
	}
	id, err := strconv.ParseInt(event.ResourceID, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonInvalidResource)
	}
	spaceID := spaceIDOf(event)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	return r.library.DeleteMediaFileInSpace(spaceID, id)
}

func parseStringMap(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, errors.New("empty")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		// 可能是 map[string]any
		var anyMap map[string]any
		if err2 := json.Unmarshal([]byte(raw), &anyMap); err2 != nil {
			return nil, err
		}
		m = make(map[string]string, len(anyMap))
		for k, v := range anyMap {
			if v == nil {
				m[k] = ""
				continue
			}
			switch t := v.(type) {
			case string:
				m[k] = t
			default:
				b, _ := json.Marshal(t)
				m[k] = string(b)
			}
		}
	}
	return m, nil
}

// --- media.renamed → 改回 before 文件名 ---

type mediaRenamedReverter struct {
	library *library.Service
}

func (r *mediaRenamedReverter) CanRollback(event *models.AuditEvent) (bool, string) {
	if event == nil || strings.TrimSpace(event.ResourceID) == "" {
		return false, ReasonInvalidResource
	}
	if _, err := strconv.ParseInt(event.ResourceID, 10, 64); err != nil {
		return false, ReasonInvalidResource
	}
	oldName, ok := oldFileNameFromEvent(event)
	if !ok {
		return false, ReasonMissingBefore
	}
	_ = oldName
	return true, ""
}

func (r *mediaRenamedReverter) Apply(_ context.Context, event *models.AuditEvent) error {
	if r == nil || r.library == nil {
		return errors.New("媒体库服务未启用")
	}
	id, err := strconv.ParseInt(event.ResourceID, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonInvalidResource)
	}
	oldName, ok := oldFileNameFromEvent(event)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonMissingBefore)
	}
	spaceID := spaceIDOf(event)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	_, err = r.library.RenameMediaFileInSpace(spaceID, id, oldName)
	return err
}

func oldFileNameFromEvent(event *models.AuditEvent) (string, bool) {
	before, err := parseAnyStringMap(event.BeforeJSON)
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(before["file_name"])
	if name == "" {
		// 从 file_path 推导
		p := strings.TrimSpace(before["file_path"])
		if p == "" || strings.Contains(p, "****") {
			return "", false
		}
		// 兼容 / 与 \
		p = strings.ReplaceAll(p, "\\", "/")
		if i := strings.LastIndex(p, "/"); i >= 0 && i+1 < len(p) {
			name = p[i+1:]
		} else {
			name = p
		}
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.Contains(name, "****") {
		return "", false
	}
	return name, true
}

// --- media.moved → 移回 before 所在目录 ---

type mediaMovedReverter struct {
	library *library.Service
}

func (r *mediaMovedReverter) CanRollback(event *models.AuditEvent) (bool, string) {
	if event == nil || strings.TrimSpace(event.ResourceID) == "" {
		return false, ReasonInvalidResource
	}
	if _, err := strconv.ParseInt(event.ResourceID, 10, 64); err != nil {
		return false, ReasonInvalidResource
	}
	_, reason, ok := oldDirFromEvent(event)
	if !ok {
		return false, reason
	}
	return true, ""
}

func (r *mediaMovedReverter) Apply(_ context.Context, event *models.AuditEvent) error {
	if r == nil || r.library == nil {
		return errors.New("媒体库服务未启用")
	}
	id, err := strconv.ParseInt(event.ResourceID, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonInvalidResource)
	}
	oldDir, reason, ok := oldDirFromEvent(event)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, reason)
	}
	spaceID := spaceIDOf(event)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	_, err = r.library.MoveMediaFileInSpace(spaceID, id, oldDir)
	return err
}

// oldDirFromEvent 从 before.file_path 取旧目录；路径经审计脱敏（含 ****）时不可回滚。
func oldDirFromEvent(event *models.AuditEvent) (dir string, reasonKey string, ok bool) {
	before, err := parseAnyStringMap(event.BeforeJSON)
	if err != nil {
		return "", ReasonMissingBefore, false
	}
	p := strings.TrimSpace(before["file_path"])
	if p == "" {
		return "", ReasonMissingBefore, false
	}
	if strings.Contains(p, "****") {
		// FR2-040 对 C:/Users/<name>/ 脱敏后无法还原真实目录
		return "", ReasonPathRedacted, false
	}
	disk := filepath.FromSlash(p)
	dir = filepath.Dir(disk)
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return "", ReasonMissingBefore, false
	}
	return dir, "", true
}

// --- metadata.writeback.succeeded → 快照覆盖原文件 ---

type writebackSucceededReverter struct{}

func (r *writebackSucceededReverter) CanRollback(event *models.AuditEvent) (bool, string) {
	if event == nil {
		return false, ReasonInvalidResource
	}
	snap, file, reason := writebackPathsFromEventDetailed(event)
	if reason != "" {
		return false, reason
	}
	if _, err := os.Stat(snap); err != nil {
		return false, ReasonSnapshotGone
	}
	_ = file
	return true, ""
}

func (r *writebackSucceededReverter) Apply(_ context.Context, event *models.AuditEvent) error {
	snap, file, reason := writebackPathsFromEventDetailed(event)
	if reason != "" {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, reason)
	}
	if _, err := os.Stat(snap); err != nil {
		return fmt.Errorf("%w: %s", ErrNotRollbackable, ReasonSnapshotGone)
	}
	return library.RestoreFileFromWritebackSnapshot(snap, file)
}

// writebackPathsFromEventDetailed 从审计 metadata/before 解析 snapshot_path 与 file_path。
// reasonKey 非空表示不可回滚原因。
func writebackPathsFromEventDetailed(event *models.AuditEvent) (snapshotPath, filePath, reasonKey string) {
	if event == nil {
		return "", "", ReasonInvalidResource
	}
	meta, _ := parseAnyStringMap(event.MetadataJSON)
	snap := strings.TrimSpace(meta["snapshot_path"])
	file := strings.TrimSpace(meta["file_path"])
	if snap == "" || file == "" {
		before, _ := parseAnyStringMap(event.BeforeJSON)
		if snap == "" {
			snap = strings.TrimSpace(before["snapshot_path"])
		}
		if file == "" {
			file = strings.TrimSpace(before["file_path"])
		}
	}
	if snap == "" || file == "" {
		return "", "", ReasonMissingSnapshot
	}
	if strings.Contains(snap, "****") || strings.Contains(file, "****") {
		return "", "", ReasonPathRedacted
	}
	return snap, file, ""
}

func parseAnyStringMap(raw string) (map[string]string, error) {
	return parseStringMap(raw)
}
