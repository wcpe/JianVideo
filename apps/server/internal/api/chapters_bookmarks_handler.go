package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/library"
)

// GetMediaChapters 返回媒体当前只读内嵌章节。
func (h *Handler) GetMediaChapters(c *gin.Context) {
	spaceID, mediaID, ok := h.chaptersBookmarksMedia(c)
	if !ok {
		return
	}
	result, err := h.library.GetMediaChapters(c.Request.Context(), spaceID, mediaID)
	if err != nil {
		writeChaptersBookmarksError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": result.Items, "stale": result.Stale, "parsed_at": result.ParsedAt})
}

// ListMediaBookmarks 返回媒体书签列表。
func (h *Handler) ListMediaBookmarks(c *gin.Context) {
	spaceID, mediaID, ok := h.chaptersBookmarksMedia(c)
	if !ok {
		return
	}
	items, err := h.library.ListMediaBookmarks(c.Request.Context(), spaceID, mediaID)
	if err != nil {
		writeChaptersBookmarksError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateMediaBookmark 创建书签。
func (h *Handler) CreateMediaBookmark(c *gin.Context) {
	spaceID, mediaID, ok := h.chaptersBookmarksMedia(c)
	if !ok {
		return
	}
	var request bookmarkWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	bookmark, err := h.library.CreateMediaBookmark(c.Request.Context(), spaceID, mediaID, request.input())
	if err != nil {
		writeChaptersBookmarksError(c, err)
		return
	}
	c.JSON(http.StatusCreated, bookmark)
}

// UpdateMediaBookmark 使用 revision CAS 更新书签。
func (h *Handler) UpdateMediaBookmark(c *gin.Context) {
	spaceID, mediaID, ok := h.chaptersBookmarksMedia(c)
	if !ok {
		return
	}
	var request bookmarkWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	bookmark, err := h.library.UpdateMediaBookmark(c.Request.Context(), spaceID, mediaID, c.Param("bookmark_id"), request.update())
	if err != nil {
		writeChaptersBookmarksError(c, err)
		return
	}
	c.JSON(http.StatusOK, bookmark)
}

// DeleteMediaBookmark 使用 revision CAS 删除书签。
func (h *Handler) DeleteMediaBookmark(c *gin.Context) {
	spaceID, mediaID, ok := h.chaptersBookmarksMedia(c)
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "revision 必须为正整数"})
		return
	}
	if err := h.library.DeleteMediaBookmark(c.Request.Context(), spaceID, mediaID, c.Param("bookmark_id"), revision); err != nil {
		writeChaptersBookmarksError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type bookmarkWriteRequest struct {
	PositionMS int64   `json:"position_ms"`
	Title      string  `json:"title"`
	Note       *string `json:"note"`
	Revision   int64   `json:"revision"`
}

func (r bookmarkWriteRequest) input() library.BookmarkInput {
	return library.BookmarkInput{PositionMS: r.PositionMS, Title: r.Title, Note: r.Note}
}

func (r bookmarkWriteRequest) update() library.BookmarkUpdate {
	return library.BookmarkUpdate{PositionMS: r.PositionMS, Title: r.Title, Note: r.Note, Revision: r.Revision}
}

func (h *Handler) chaptersBookmarksMedia(c *gin.Context) (string, int64, bool) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return "", 0, false
	}
	mediaID, ok := parseMediaID(c)
	return spaceID, mediaID, ok
}

func writeChaptersBookmarksError(c *gin.Context, err error) {
	var conflict *library.BookmarkConflictError
	if errors.As(err, &conflict) {
		c.JSON(http.StatusConflict, gin.H{"code": "BOOKMARK_CONFLICT", "message": err.Error(), "current": conflict.Current, "deleted": conflict.Deleted})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "MEDIA_NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	status, code, message := bookmarkValidationError(err)
	if status != 0 {
		c.JSON(status, gin.H{"code": code, "message": message})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "章节或书签操作失败"})
}

func bookmarkValidationError(err error) (int, string, string) {
	switch {
	case errors.Is(err, library.ErrBookmarkInvalidPosition):
		return http.StatusBadRequest, "BOOKMARK_INVALID_POSITION", err.Error()
	case errors.Is(err, library.ErrBookmarkTitleRequired):
		return http.StatusBadRequest, "BOOKMARK_TITLE_REQUIRED", err.Error()
	case errors.Is(err, library.ErrBookmarkTitleTooLong):
		return http.StatusBadRequest, "BOOKMARK_TITLE_TOO_LONG", err.Error()
	case errors.Is(err, library.ErrBookmarkNoteTooLong):
		return http.StatusBadRequest, "BOOKMARK_NOTE_TOO_LONG", err.Error()
	default:
		return 0, "", ""
	}
}
