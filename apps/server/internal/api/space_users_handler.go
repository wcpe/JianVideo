package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/space"
)

// WithAuth 注入认证服务（用户管理）。
func (h *Handler) WithAuth(svc *auth.Service) *Handler {
	h.auth = svc
	return h
}

// WithSpace 注入 Space 成员服务。
func (h *Handler) WithSpace(svc *space.Service) *Handler {
	h.space = svc
	return h
}

// ListUsers 列出用户（默认 Space owner）。
func (h *Handler) ListUsers(c *gin.Context) {
	if h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUTH_UNAVAILABLE", "message": "认证服务未启用"})
		return
	}
	if !h.requireDefaultSpaceOwner(c) {
		return
	}
	users, err := h.auth.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(users))
	for _, u := range users {
		items = append(items, gin.H{
			"id":         u.ID,
			"username":   u.Username,
			"status":     u.Status,
			"created_at": u.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateUser 创建用户（默认 Space owner）。
func (h *Handler) CreateUser(c *gin.Context) {
	if h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUTH_UNAVAILABLE", "message": "认证服务未启用"})
		return
	}
	if !h.requireDefaultSpaceOwner(c) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	user, err := h.auth.CreateUser(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "CREATE_USER_FAILED", "message": err.Error()})
		return
	}
	h.recordSpaceAudit(c, audit.EventInput{
		Scope:        audit.ScopeSystem,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       "user.created",
		ResourceType: "user",
		ResourceID:   fmt.Sprintf("%d", user.ID),
		After:        map[string]any{"username": user.Username, "status": user.Status},
	})
	c.JSON(http.StatusCreated, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"status":   user.Status,
	})
}

// SetUserStatus 启用/禁用用户。
func (h *Handler) SetUserStatus(c *gin.Context) {
	if h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUTH_UNAVAILABLE", "message": "认证服务未启用"})
		return
	}
	if !h.requireDefaultSpaceOwner(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的用户 ID"})
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	// 禁止禁用自己（须解析 user_id；/api/users 不经 Space 守卫）。
	if req.Status == models.UserStatusDisabled {
		selfID, ok := h.currentUserID(c)
		if !ok {
			return
		}
		if selfID == id {
			c.JSON(http.StatusBadRequest, gin.H{"code": "CANNOT_DISABLE_SELF", "message": "不能禁用当前登录用户"})
			return
		}
	}
	before, _ := h.auth.FindUserByID(id)
	if err := h.auth.SetUserStatus(id, req.Status); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SET_STATUS_FAILED", "message": err.Error()})
		return
	}
	// 禁用时撤销该用户全部会话，堵住旧 JWT（FR2-062 后置）。
	var revokedCount int64
	if req.Status == models.UserStatusDisabled && h.auth.SessionTableReady() {
		n, revErr := h.auth.RevokeAllSessions(int64(id))
		if revErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "禁用成功但撤销会话失败"})
			return
		}
		revokedCount = n
	}
	action := "user.status_changed"
	if req.Status == models.UserStatusDisabled {
		action = "user.disabled"
	}
	auditIn := audit.EventInput{
		Scope:        audit.ScopeSystem,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       action,
		ResourceType: "user",
		ResourceID:   fmt.Sprintf("%d", id),
		Before:       userAuditPayload(before),
		After:        map[string]any{"status": req.Status},
	}
	if req.Status == models.UserStatusDisabled {
		auditIn.Metadata = map[string]any{"sessions_revoked": revokedCount}
	}
	h.recordSpaceAudit(c, auditIn)
	c.Status(http.StatusNoContent)
}

// RevokeUserSessions 管理员撤销指定用户全部会话（FR2-062 后置）。
// 守卫与 ListUsers 相同：默认 Space owner；允许撤自己的全部（强制重登）。
func (h *Handler) RevokeUserSessions(c *gin.Context) {
	if h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUTH_UNAVAILABLE", "message": "认证服务未启用"})
		return
	}
	if !h.requireDefaultSpaceOwner(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的用户 ID"})
		return
	}
	if !h.auth.SessionTableReady() {
		// 无会话表时视为无可撤会话
		c.JSON(http.StatusOK, gin.H{"revoked": 0})
		return
	}
	n, err := h.auth.RevokeAllSessions(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "撤销会话失败"})
		return
	}
	h.recordSpaceAudit(c, audit.EventInput{
		Scope:        audit.ScopeSystem,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       "auth.sessions_revoked",
		ResourceType: "user",
		ResourceID:   fmt.Sprintf("%d", id),
		Metadata:     map[string]any{"count": n},
	})
	// 若撤的是自己，清 Cookie 强制重登
	if selfID, ok := h.currentUserID(c); ok && int64(selfID) == id {
		auth.ClearAuthCookie(c)
	}
	c.JSON(http.StatusOK, gin.H{"revoked": n})
}

// ListSpaces 列出当前用户可访问的 Space。
func (h *Handler) ListSpaces(c *gin.Context) {
	if h.space == nil || h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	spaces, err := h.space.ListAccessible(int64(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(spaces))
	for _, sp := range spaces {
		role, _ := h.space.MemberRole(sp.ID, int64(userID))
		items = append(items, gin.H{
			"id":            sp.ID,
			"name":          sp.Name,
			"owner_user_id": sp.OwnerUserID,
			"role":          role,
			"created_at":    sp.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateSpace 创建 Space（创建者为 owner）。
func (h *Handler) CreateSpace(c *gin.Context) {
	if h.space == nil || h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	sp, err := h.space.CreateSpace(req.ID, req.Name, int64(userID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "CREATE_SPACE_FAILED", "message": err.Error()})
		return
	}
	h.recordSpaceAudit(c, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      sp.ID,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       "space.created",
		ResourceType: "space",
		ResourceID:   sp.ID,
		After:        map[string]any{"name": sp.Name, "owner_user_id": sp.OwnerUserID},
	})
	c.JSON(http.StatusCreated, gin.H{
		"id":            sp.ID,
		"name":          sp.Name,
		"owner_user_id": sp.OwnerUserID,
		"role":          models.SpaceRoleOwner,
	})
}

// ListSpaceMembers 列出成员（需至少 viewer）。
func (h *Handler) ListSpaceMembers(c *gin.Context) {
	if h.space == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	spaceID := strings.TrimSpace(c.Param("id"))
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if err := h.space.RequireRole(spaceID, int64(userID), models.SpaceRoleViewer); err != nil {
		writeSpaceErr(c, err)
		return
	}
	members, err := h.space.ListMembers(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": members})
}

// AddSpaceMember 添加/更新成员（owner）。
func (h *Handler) AddSpaceMember(c *gin.Context) {
	if h.space == nil || h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	spaceID := strings.TrimSpace(c.Param("id"))
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if err := h.space.RequireRole(spaceID, int64(userID), models.SpaceRoleOwner); err != nil {
		writeSpaceErr(c, err)
		return
	}
	var req struct {
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	targetID := req.UserID
	if targetID <= 0 && strings.TrimSpace(req.Username) != "" {
		u, err := h.auth.FindUserByUsername(strings.TrimSpace(req.Username))
		if err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "USER_NOT_FOUND", "message": "用户不存在"})
			return
		}
		targetID = int64(u.ID)
	}
	if targetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_USER", "message": "需要 user_id 或 username"})
		return
	}
	created, err := h.space.AddMember(spaceID, targetID, req.Role)
	if err != nil {
		writeSpaceErr(c, err)
		return
	}
	action := "space.member_added"
	if !created {
		action = "space.member_role_changed"
	}
	h.recordSpaceAudit(c, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      spaceID,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       action,
		ResourceType: "space_member",
		ResourceID:   fmt.Sprintf("%s:%d", spaceID, targetID),
		After:        map[string]any{"user_id": targetID, "role": req.Role},
	})
	c.Status(http.StatusNoContent)
}

// RemoveSpaceMember 移除成员（owner）。
func (h *Handler) RemoveSpaceMember(c *gin.Context) {
	if h.space == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	spaceID := strings.TrimSpace(c.Param("id"))
	memberID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || memberID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的用户 ID"})
		return
	}
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if err := h.space.RequireRole(spaceID, int64(userID), models.SpaceRoleOwner); err != nil {
		writeSpaceErr(c, err)
		return
	}
	if err := h.space.RemoveMember(spaceID, memberID); err != nil {
		writeSpaceErr(c, err)
		return
	}
	h.recordSpaceAudit(c, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      spaceID,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       "space.member_removed",
		ResourceType: "space_member",
		ResourceID:   fmt.Sprintf("%s:%d", spaceID, memberID),
		After:        map[string]any{"user_id": memberID},
	})
	c.Status(http.StatusNoContent)
}

// TransferSpaceOwner 转让 Space 所有权（仅当前 owner；to 须已是成员）。
// POST /api/spaces/:id/transfer-owner  body: {"user_id": <int64>}
func (h *Handler) TransferSpaceOwner(c *gin.Context) {
	if h.space == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	spaceID := strings.TrimSpace(c.Param("id"))
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	// 双保险：中间件已 RequireSpaceRole(owner)，handler 再校验一次。
	if err := h.space.RequireRole(spaceID, int64(userID), models.SpaceRoleOwner); err != nil {
		writeSpaceErr(c, err)
		return
	}
	var req struct {
		UserID int64 `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效，需要 user_id"})
		return
	}
	beforeOwner := int64(userID)
	if err := h.space.TransferOwner(spaceID, int64(userID), req.UserID); err != nil {
		writeSpaceErr(c, err)
		return
	}
	h.recordSpaceAudit(c, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      spaceID,
		ActorType:    "user",
		ActorID:      actorIDFromContext(c),
		Action:       "space.owner_transferred",
		ResourceType: "space",
		ResourceID:   spaceID,
		Metadata: map[string]any{
			"before_owner_user_id": beforeOwner,
			"after_owner_user_id":  req.UserID,
		},
	})
	c.Status(http.StatusNoContent)
}

func (h *Handler) requireDefaultSpaceOwner(c *gin.Context) bool {
	username, ok := c.Get("username")
	name, valid := username.(string)
	if !ok || !valid || strings.TrimSpace(name) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "请先登录"})
		return false
	}
	isOwner, err := h.auth.IsDefaultSpaceOwner(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "校验权限失败"})
		return false
	}
	if !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "仅默认 Space owner 可管理用户"})
		return false
	}
	return true
}

func (h *Handler) currentUserID(c *gin.Context) (int, bool) {
	if v, ok := c.Get("user_id"); ok {
		switch id := v.(type) {
		case int:
			return id, true
		case int64:
			return int(id), true
		}
	}
	username, ok := c.Get("username")
	name, valid := username.(string)
	if !ok || !valid || h.auth == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "请先登录"})
		return 0, false
	}
	u, err := h.auth.FindUserByUsername(name)
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "请先登录"})
		return 0, false
	}
	c.Set("user_id", u.ID)
	return u.ID, true
}

func (h *Handler) recordSpaceAudit(c *gin.Context, input audit.EventInput) {
	if h.audit == nil {
		return
	}
	if input.ActorID == "" {
		input.ActorID = actorIDFromContext(c)
	}
	_ = h.audit.Record(c.Request.Context(), input)
}

func actorIDFromContext(c *gin.Context) string {
	if v, ok := c.Get("user_id"); ok {
		switch id := v.(type) {
		case int:
			return fmt.Sprintf("%d", id)
		case int64:
			return fmt.Sprintf("%d", id)
		}
	}
	if v, ok := c.Get("username"); ok {
		if name, ok := v.(string); ok {
			return name
		}
	}
	return ""
}

func userAuditPayload(u *models.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{"id": u.ID, "username": u.Username, "status": u.Status}
}

func writeSpaceErr(c *gin.Context, err error) {
	switch err {
	case space.ErrSpaceNotFound:
		c.JSON(http.StatusNotFound, gin.H{"code": "SPACE_NOT_FOUND", "message": "Space 不存在"})
	case space.ErrNotMember:
		c.JSON(http.StatusForbidden, gin.H{"code": "SPACE_FORBIDDEN", "message": "不是该 Space 的成员"})
	case space.ErrForbidden:
		c.JSON(http.StatusForbidden, gin.H{"code": "SPACE_FORBIDDEN", "message": "当前角色无权执行此操作"})
	case space.ErrInvalidRole:
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ROLE", "message": "角色无效"})
	case space.ErrCannotRemoveOwner:
		c.JSON(http.StatusBadRequest, gin.H{"code": "CANNOT_REMOVE_OWNER", "message": "不能移除 Space owner"})
	case space.ErrCannotTransferToSelf:
		c.JSON(http.StatusBadRequest, gin.H{"code": "CANNOT_TRANSFER_TO_SELF", "message": "不能转让给自己"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "SPACE_ERROR", "message": err.Error()})
	}
}
