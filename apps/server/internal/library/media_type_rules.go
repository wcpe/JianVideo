package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

// 预留媒体类型常量。
const (
	MediaTypeAudio    = "audio"
	MediaTypeSubtitle = "subtitle"
	MediaTypeSidecar  = "sidecar"
)

const (
	capabilityScan      = "scan"
	capabilityTranscode = "transcode"
	capabilityThumbnail = "thumbnail"
	capabilityMetadata  = "metadata"
)

// MediaTypeDefinition 描述一种可配置的媒体类型。
type MediaTypeDefinition struct {
	Type              string   `json:"type"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	DefaultExtensions []string `json:"default_extensions"`
	Capabilities      []string `json:"capabilities"`
}

// MediaTypeRuleView 是前端可见的媒体类型规则。
type MediaTypeRuleView struct {
	ID           string   `json:"id"`
	SpaceID      string   `json:"space_id"`
	LibraryID    *int64   `json:"library_id,omitempty"`
	Type         string   `json:"type"`
	Extension    string   `json:"extension"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Enabled      bool     `json:"enabled"`
	Builtin      bool     `json:"builtin"`
	Capabilities []string `json:"capabilities"`
}

// MediaTypesResponse 汇总媒体类型定义与后缀规则。
type MediaTypesResponse struct {
	Types []MediaTypeDefinition `json:"types"`
	Rules []MediaTypeRuleView   `json:"rules"`
}

// MediaTypeRuleInput 是新增媒体类型规则的输入。
type MediaTypeRuleInput struct {
	LibraryID   *int64 `json:"library_id"`
	Type        string `json:"type"`
	Extension   string `json:"extension"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

// MediaTypeRuleUpdate 是更新媒体类型规则的输入。
type MediaTypeRuleUpdate struct {
	LibraryID   *int64  `json:"library_id"`
	Enabled     *bool   `json:"enabled"`
	Label       *string `json:"label"`
	Description *string `json:"description"`
}

var mediaTypeDefinitions = []MediaTypeDefinition{
	{Type: MediaTypeVideo, Name: "视频", Description: "可扫描、预览、转码并提取技术元数据的视频文件。", Capabilities: []string{capabilityScan, capabilityTranscode, capabilityThumbnail, capabilityMetadata}},
	{Type: MediaTypeImage, Name: "图片", Description: "可扫描、生成缩略图并提取 EXIF 元数据的图片文件。", Capabilities: []string{capabilityScan, capabilityThumbnail, capabilityMetadata}},
	{Type: MediaTypeAudio, Name: "音频", Description: "预留的音频素材类型，本版本不启用扫描处理。", Capabilities: []string{}},
	{Type: MediaTypeSubtitle, Name: "字幕", Description: "预留的字幕素材类型，本版本不启用独立扫描处理。", Capabilities: []string{}},
	{Type: MediaTypeSidecar, Name: "伴随文件", Description: "预留的 sidecar 元数据类型，本版本不启用独立扫描处理。", Capabilities: []string{}},
}

var mediaTypeDefinitionByType = map[string]MediaTypeDefinition{}

func init() {
	defaultExts := map[string][]string{}
	for ext, mediaType := range builtInMediaExtensions {
		defaultExts[mediaType] = append(defaultExts[mediaType], ext)
	}
	for i := range mediaTypeDefinitions {
		sort.Strings(defaultExts[mediaTypeDefinitions[i].Type])
		mediaTypeDefinitions[i].DefaultExtensions = defaultExts[mediaTypeDefinitions[i].Type]
		mediaTypeDefinitionByType[mediaTypeDefinitions[i].Type] = mediaTypeDefinitions[i]
	}
}

// ListMediaTypesInSpace 返回指定空间的媒体类型定义与规则。
func (s *Service) ListMediaTypesInSpace(spaceID string, libraryID int64) (MediaTypesResponse, error) {
	spaceID = normalizeSpaceID(spaceID)
	if libraryID > 0 {
		if _, err := s.GetLibraryPathByIDInSpace(spaceID, libraryID); err != nil {
			return MediaTypesResponse{}, err
		}
	}
	rules, err := s.resolveMediaTypeRules(spaceID, libraryID, true)
	if err != nil {
		return MediaTypesResponse{}, err
	}
	return MediaTypesResponse{Types: mediaTypeDefinitions, Rules: rules}, nil
}

// CreateMediaTypeRuleInSpace 新增或覆盖指定空间的媒体类型规则。
func (s *Service) CreateMediaTypeRuleInSpace(spaceID string, input MediaTypeRuleInput) (MediaTypeRuleView, error) {
	spaceID = normalizeSpaceID(spaceID)
	ext, mediaType, err := validateMediaRuleInput(input.Extension, input.Type)
	if err != nil {
		return MediaTypeRuleView{}, err
	}
	if err := s.validateMediaRuleLibrary(spaceID, input.LibraryID); err != nil {
		return MediaTypeRuleView{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if builtinType, ok := builtInMediaExtensions[ext]; ok && builtinType != mediaType {
		return MediaTypeRuleView{}, fmt.Errorf("内置后缀 %s 属于 %s", ext, builtinType)
	}
	builtin := builtInMediaExtensions[ext] == mediaType
	if builtin && input.Enabled == nil && input.Label == "" && input.Description == "" {
		return s.builtinMediaRuleView(spaceID, input.LibraryID, mediaType, ext, enabled), nil
	}
	model, err := s.upsertMediaTypeRule(spaceID, input.LibraryID, mediaType, ext, enabled, builtin, input.Label, input.Description, "media_type_rule.created")
	if err != nil {
		return MediaTypeRuleView{}, err
	}
	return s.modelToMediaTypeRuleView(model), nil
}

// UpdateMediaTypeRuleInSpace 更新指定空间的媒体类型规则。
func (s *Service) UpdateMediaTypeRuleInSpace(spaceID, id string, input MediaTypeRuleUpdate) (MediaTypeRuleView, error) {
	spaceID = normalizeSpaceID(spaceID)
	if mediaType, ext, ok := parseBuiltinRuleID(id); ok {
		if err := s.validateMediaRuleLibrary(spaceID, input.LibraryID); err != nil {
			return MediaTypeRuleView{}, err
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		label, description := derefString(input.Label), derefString(input.Description)
		model, err := s.upsertMediaTypeRule(spaceID, input.LibraryID, mediaType, ext, enabled, true, label, description, "media_type_rule.updated")
		if err != nil {
			return MediaTypeRuleView{}, err
		}
		return s.modelToMediaTypeRuleView(model), nil
	}
	return s.updatePersistedMediaTypeRule(spaceID, id, input)
}

// DeleteMediaTypeRuleInSpace 删除指定空间的自定义媒体类型规则。
func (s *Service) DeleteMediaTypeRuleInSpace(spaceID, id string) error {
	spaceID = normalizeSpaceID(spaceID)
	if _, _, ok := parseBuiltinRuleID(id); ok {
		return fmt.Errorf("内置规则不可删除")
	}
	ruleID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || ruleID <= 0 {
		return fmt.Errorf("规则 ID 无效")
	}
	return s.mediaTypeRuleRepo.RunInTx(func(tx *gorm.DB) error {
		rule, err := s.mediaTypeRuleRepo.GetByIDTx(tx, spaceID, ruleID)
		if err != nil {
			return err
		}
		if rule.Builtin {
			return fmt.Errorf("内置规则不可删除")
		}
		if err := s.mediaTypeRuleRepo.DeleteTx(tx, rule); err != nil {
			return err
		}
		return s.recordMediaTypeRuleAudit(tx, "media_type_rule.deleted", rule, nil)
	})
}

// EnabledExtensionsForType 返回指定媒体类型当前启用的后缀。
func (s *Service) EnabledExtensionsForType(spaceID string, libraryID int64, mediaType string) ([]string, error) {
	rules, err := s.resolveMediaTypeRules(spaceID, libraryID, false)
	if err != nil {
		return nil, err
	}
	exts := make([]string, 0)
	for _, rule := range rules {
		if rule.Type == mediaType && rule.Enabled {
			exts = append(exts, rule.Extension)
		}
	}
	sort.Strings(exts)
	return exts, nil
}

// MediaTypeByPathInSpace 根据空间、媒体库和路径判断媒体类型。
func (s *Service) MediaTypeByPathInSpace(spaceID string, libraryID int64, path string) (string, bool) {
	policy, err := s.mediaExtensionPolicyInSpace(spaceID, libraryID)
	if err != nil {
		return "", false
	}
	return policy.MediaTypeByPath(path)
}

func (s *Service) mediaExtensionPolicyInSpace(spaceID string, libraryID int64) (mediaExtensionPolicy, error) {
	rules, err := s.resolveMediaTypeRules(spaceID, libraryID, false)
	if err != nil {
		return nil, err
	}
	policy := make(mediaExtensionPolicy, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			policy[rule.Extension] = rule.Type
		}
	}
	return policy, nil
}

func (s *Service) resolveMediaTypeRules(spaceID string, libraryID int64, includeDisabled bool) ([]MediaTypeRuleView, error) {
	base := builtinMediaRuleMap(spaceID, libraryID)
	if s.mediaTypeRuleRepo == nil || !s.mediaTypeRuleRepo.HasTable() {
		if err := s.applyLegacyMediaExtensions(base, libraryID); err != nil {
			return nil, err
		}
		return sortedMediaRuleViews(base, includeDisabled), nil
	}
	rows, err := s.mediaTypeRuleRepo.ListBySpace(spaceID, libraryID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		applyPersistedMediaTypeRule(base, row)
	}
	return sortedMediaRuleViews(base, includeDisabled), nil
}

func (s *Service) applyLegacyMediaExtensions(rules map[string]MediaTypeRuleView, libraryID int64) error {
	if s.pathRepo == nil || libraryID <= 0 {
		return nil
	}
	if !s.pathRepo.HasMediaExtensionTable() {
		return nil
	}
	custom, err := s.listCustomMediaExtensions(libraryID)
	if err != nil {
		return err
	}
	for _, item := range custom {
		libraryID := item.LibraryID
		rules[mediaRuleKey(item.Type, item.Extension)] = MediaTypeRuleView{
			ID:           strconv.FormatInt(item.ID, 10),
			SpaceID:      models.DefaultSpaceID,
			LibraryID:    &libraryID,
			Type:         item.Type,
			Extension:    item.Extension,
			Label:        mediaRuleLabel(item.Type, item.Extension),
			Description:  mediaRuleDescription(item.Type, item.Extension),
			Enabled:      true,
			Builtin:      item.IsBuiltIn == 1,
			Capabilities: defaultCapabilities(item.Type),
		}
	}
	return nil
}

func builtinMediaRuleMap(spaceID string, libraryID int64) map[string]MediaTypeRuleView {
	rules := make(map[string]MediaTypeRuleView, len(builtInMediaExtensions))
	for ext, mediaType := range builtInMediaExtensions {
		rules[mediaRuleKey(mediaType, ext)] = builtinRuleView(spaceID, libraryID, mediaType, ext, true)
	}
	return rules
}

func applyPersistedMediaTypeRule(rules map[string]MediaTypeRuleView, row models.MediaTypeRule) {
	view := modelToMediaTypeRuleView(row)
	if row.Builtin {
		view.ID = builtinRuleID(row.Type, row.Extension)
		view.Builtin = true
	}
	rules[mediaRuleKey(view.Type, view.Extension)] = view
}

func sortedMediaRuleViews(ruleMap map[string]MediaTypeRuleView, includeDisabled bool) []MediaTypeRuleView {
	rules := make([]MediaTypeRuleView, 0, len(ruleMap))
	for _, rule := range ruleMap {
		if includeDisabled || rule.Enabled {
			rules = append(rules, rule)
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Type == rules[j].Type {
			return rules[i].Extension < rules[j].Extension
		}
		return rules[i].Type < rules[j].Type
	})
	return rules
}

func validateMediaRuleInput(extension, mediaType string) (string, string, error) {
	ext := normalizeExtension(extension)
	mediaType = strings.TrimSpace(mediaType)
	if !isAllowedCustomExtension(ext) {
		return "", "", fmt.Errorf("后缀格式不支持")
	}
	if _, ok := mediaTypeDefinitionByType[mediaType]; !ok {
		return "", "", fmt.Errorf("媒体类型不支持")
	}
	return ext, mediaType, nil
}

func (s *Service) validateMediaRuleLibrary(spaceID string, libraryID *int64) error {
	if libraryID == nil || *libraryID <= 0 {
		return nil
	}
	_, err := s.GetLibraryPathByIDInSpace(spaceID, *libraryID)
	return err
}

func (s *Service) upsertMediaTypeRule(spaceID string, libraryID *int64, mediaType, ext string, enabled, builtin bool, label, description, action string) (*models.MediaTypeRule, error) {
	var saved models.MediaTypeRule
	err := s.mediaTypeRuleRepo.RunInTx(func(tx *gorm.DB) error {
		rule := models.MediaTypeRule{
			SpaceID:          spaceID,
			LibraryID:        libraryID,
			Type:             mediaType,
			Extension:        ext,
			Label:            strings.TrimSpace(label),
			Description:      strings.TrimSpace(description),
			Enabled:          enabled,
			Builtin:          builtin,
			CapabilitiesJSON: encodeCapabilities(defaultCapabilities(mediaType)),
		}
		existing, err := s.mediaTypeRuleRepo.FindByKeyTx(tx, spaceID, libraryID, mediaType, ext)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := s.mediaTypeRuleRepo.CreateTx(tx, &rule); err != nil {
				return err
			}
			saved = rule
		} else {
			rule.ID = existing.ID
			if err := s.mediaTypeRuleRepo.UpdateFieldsTx(tx, existing.ID, mediaTypeRuleUpdates(rule)); err != nil {
				return err
			}
			saved = *existing
			applyMediaTypeRuleUpdates(&saved, rule)
		}
		return s.recordMediaTypeRuleAudit(tx, action, nil, &saved)
	})
	return &saved, err
}

func (s *Service) updatePersistedMediaTypeRule(spaceID, id string, input MediaTypeRuleUpdate) (MediaTypeRuleView, error) {
	ruleID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || ruleID <= 0 {
		return MediaTypeRuleView{}, fmt.Errorf("规则 ID 无效")
	}
	var saved models.MediaTypeRule
	err = s.mediaTypeRuleRepo.RunInTx(func(tx *gorm.DB) error {
		rule, err := s.mediaTypeRuleRepo.GetByIDTx(tx, spaceID, ruleID)
		if err != nil {
			return err
		}
		before := *rule
		updates := map[string]any{}
		if input.Enabled != nil {
			updates["enabled"] = *input.Enabled
		}
		if input.Label != nil {
			updates["label"] = strings.TrimSpace(*input.Label)
		}
		if input.Description != nil {
			updates["description"] = strings.TrimSpace(*input.Description)
		}
		if len(updates) > 0 {
			if err := s.mediaTypeRuleRepo.UpdateFieldsTx(tx, rule.ID, updates); err != nil {
				return err
			}
			reloaded, err := s.mediaTypeRuleRepo.ReloadTx(tx, rule.ID)
			if err != nil {
				return err
			}
			rule = reloaded
		}
		saved = *rule
		return s.recordMediaTypeRuleAudit(tx, "media_type_rule.updated", &before, &saved)
	})
	if err != nil {
		return MediaTypeRuleView{}, err
	}
	return s.modelToMediaTypeRuleView(&saved), nil
}

func mediaTypeRuleUpdates(rule models.MediaTypeRule) map[string]any {
	return map[string]any{
		"label":             rule.Label,
		"description":       rule.Description,
		"enabled":           rule.Enabled,
		"builtin":           rule.Builtin,
		"capabilities_json": rule.CapabilitiesJSON,
	}
}

func applyMediaTypeRuleUpdates(dst *models.MediaTypeRule, src models.MediaTypeRule) {
	dst.Label = src.Label
	dst.Description = src.Description
	dst.Enabled = src.Enabled
	dst.Builtin = src.Builtin
	dst.CapabilitiesJSON = src.CapabilitiesJSON
}

func (s *Service) builtinMediaRuleView(spaceID string, libraryID *int64, mediaType, ext string, enabled bool) MediaTypeRuleView {
	var id int64
	if libraryID != nil {
		id = *libraryID
	}
	return builtinRuleView(spaceID, id, mediaType, ext, enabled)
}

func builtinRuleView(spaceID string, libraryID int64, mediaType, ext string, enabled bool) MediaTypeRuleView {
	var ptr *int64
	if libraryID > 0 {
		ptr = &libraryID
	}
	return MediaTypeRuleView{
		ID:           builtinRuleID(mediaType, ext),
		SpaceID:      normalizeSpaceID(spaceID),
		LibraryID:    ptr,
		Type:         mediaType,
		Extension:    ext,
		Label:        mediaRuleLabel(mediaType, ext),
		Description:  mediaRuleDescription(mediaType, ext),
		Enabled:      enabled,
		Builtin:      true,
		Capabilities: defaultCapabilities(mediaType),
	}
}

func (s *Service) modelToMediaTypeRuleView(rule *models.MediaTypeRule) MediaTypeRuleView {
	if rule == nil {
		return MediaTypeRuleView{}
	}
	return modelToMediaTypeRuleView(*rule)
}

func modelToMediaTypeRuleView(rule models.MediaTypeRule) MediaTypeRuleView {
	id := strconv.FormatInt(rule.ID, 10)
	if rule.Builtin {
		id = builtinRuleID(rule.Type, rule.Extension)
	}
	return MediaTypeRuleView{
		ID:           id,
		SpaceID:      normalizeSpaceID(rule.SpaceID),
		LibraryID:    rule.LibraryID,
		Type:         rule.Type,
		Extension:    rule.Extension,
		Label:        fallbackString(rule.Label, mediaRuleLabel(rule.Type, rule.Extension)),
		Description:  fallbackString(rule.Description, mediaRuleDescription(rule.Type, rule.Extension)),
		Enabled:      rule.Enabled,
		Builtin:      rule.Builtin,
		Capabilities: decodeCapabilities(rule.Type, rule.CapabilitiesJSON),
	}
}

func defaultCapabilities(mediaType string) []string {
	if def, ok := mediaTypeDefinitionByType[mediaType]; ok {
		return append([]string(nil), def.Capabilities...)
	}
	return nil
}

func encodeCapabilities(caps []string) string {
	raw, err := json.Marshal(caps)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeCapabilities(mediaType, raw string) []string {
	var caps []string
	if err := json.Unmarshal([]byte(raw), &caps); err == nil {
		return caps
	}
	return defaultCapabilities(mediaType)
}

func mediaRuleLabel(mediaType, ext string) string {
	if def, ok := mediaTypeDefinitionByType[mediaType]; ok {
		return strings.ToUpper(ext) + " " + def.Name
	}
	return strings.ToUpper(ext)
}

func mediaRuleDescription(mediaType, ext string) string {
	if def, ok := mediaTypeDefinitionByType[mediaType]; ok {
		return ext + " " + def.Description
	}
	return ext + " 媒体规则"
}

func mediaRuleKey(mediaType, ext string) string {
	return mediaType + "\x00" + ext
}

func builtinRuleID(mediaType, ext string) string {
	return "builtin:" + mediaType + ":" + ext
}

func parseBuiltinRuleID(id string) (string, string, bool) {
	if strings.HasPrefix(id, "builtin:") {
		parts := strings.Split(id, ":")
		return parseBuiltinRuleParts(parts)
	}
	if strings.HasPrefix(id, "builtin-") {
		parts := strings.Split(strings.TrimPrefix(id, "builtin-"), "-")
		if len(parts) == 2 {
			return parts[0], normalizeExtension(parts[1]), builtInMediaExtensions[normalizeExtension(parts[1])] == parts[0]
		}
	}
	return "", "", false
}

func parseBuiltinRuleParts(parts []string) (string, string, bool) {
	if len(parts) != 3 {
		return "", "", false
	}
	mediaType, ext := parts[1], normalizeExtension(parts[2])
	return mediaType, ext, builtInMediaExtensions[ext] == mediaType
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func boolPtr(value bool) *bool {
	return &value
}

func mediaExtensionFromRule(libraryID int64, rule MediaTypeRuleView) models.MediaExtension {
	isBuiltIn := 0
	if rule.Builtin {
		isBuiltIn = 1
	}
	return models.MediaExtension{
		LibraryID: libraryID,
		Extension: rule.Extension,
		Type:      rule.Type,
		IsBuiltIn: isBuiltIn,
	}
}

func sortedMediaExtensions(items []models.MediaExtension) []models.MediaExtension {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Extension == items[j].Extension {
			return items[i].IsBuiltIn > items[j].IsBuiltIn
		}
		return items[i].Extension < items[j].Extension
	})
	return items
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Service) recordMediaTypeRuleAudit(tx *gorm.DB, action string, before, after *models.MediaTypeRule) error {
	var rule *models.MediaTypeRule
	if after != nil {
		rule = after
	} else {
		rule = before
	}
	if rule == nil {
		return nil
	}
	return s.recordAuditTx(tx, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      normalizeSpaceID(rule.SpaceID),
		ActorType:    audit.ActorSystem,
		Action:       action,
		ResourceType: "media_type_rule",
		ResourceID:   mediaTypeRuleResourceID(rule),
		Before:       mediaTypeRuleAuditPayload(before),
		After:        mediaTypeRuleAuditPayload(after),
		Metadata:     map[string]any{"message": "媒体类型规则已变更"},
	})
}

func mediaTypeRuleResourceID(rule *models.MediaTypeRule) string {
	if rule == nil {
		return ""
	}
	if rule.Builtin {
		return builtinRuleID(rule.Type, rule.Extension)
	}
	return strconv.FormatInt(rule.ID, 10)
}

func mediaTypeRuleAuditPayload(rule *models.MediaTypeRule) map[string]any {
	if rule == nil {
		return nil
	}
	return map[string]any{
		"id":          mediaTypeRuleResourceID(rule),
		"space_id":    rule.SpaceID,
		"library_id":  rule.LibraryID,
		"type":        rule.Type,
		"extension":   rule.Extension,
		"label":       rule.Label,
		"description": rule.Description,
		"enabled":     rule.Enabled,
		"builtin":     rule.Builtin,
	}
}

func resolveThumbnailJobForMediaType(filePath, mediaType string) (thumbnailJob, bool) {
	if needsMagickConvert(filePath) {
		return thumbnailJob{filePath: filePath, kind: kindMagick}, true
	}
	switch mediaType {
	case MediaTypeImage:
		return thumbnailJob{filePath: filePath, kind: kindImage}, true
	case MediaTypeVideo:
		return thumbnailJob{filePath: filePath, kind: kindVideo}, true
	default:
		return thumbnailJob{}, false
	}
}

// GenerateThumbnailSizeInSpace 按指定空间和媒体库生成目标尺寸缩略图。
func (s *Service) GenerateThumbnailSizeInSpace(spaceID string, libraryID int64, filePath string, size int) {
	mediaType, ok := s.MediaTypeByPathInSpace(spaceID, libraryID, filePath)
	if !ok {
		return
	}
	job, ok := resolveThumbnailJobForMediaType(filePath, mediaType)
	if !ok {
		return
	}
	job.size = normalizeThumbnailSize(size)
	submitThumbnail(job)
}

func (p mediaExtensionPolicy) MediaTypeByExtension(ext string) (string, bool) {
	mediaType, ok := p[normalizeExtension(ext)]
	return mediaType, ok
}

func mediaTypeByPathFromBuiltins(path string) (string, bool) {
	return mediaTypeByExtension(normalizeExtension(filepath.Ext(path)))
}
