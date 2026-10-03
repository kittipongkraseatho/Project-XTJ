package phase1http

import (
	"errors"
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/journal_entry"
	"github.com/trading-journal/api/internal/domain/tag"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// ── Journal ────────────────────────────────────────────────────────────

type JournalUpsertInput struct {
	Strategy    *string                  `json:"strategy"`
	EntryReason *string                  `json:"entry_reason"`
	ExitReason  *string                  `json:"exit_reason"`
	Emotion     *journal_entry.Emotion   `json:"emotion"`
	Mistake     *string                  `json:"mistake"`
	Notes       *string                  `json:"notes"`
}

func (h *Handler) GetJournal(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tradeID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	// Verify trade belongs to tenant
	t, err := h.store.Trades.GetByID(c.Request().Context(), tradeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if t.TenantID != tid || t.UserID != uid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}
	entry, err := h.store.JournalEntries.GetByTradeID(c.Request().Context(), tradeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNoContent, nil)
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, entry)
}

func (h *Handler) UpsertJournal(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tradeID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	// Tenant + ownership check
	t, err := h.store.Trades.GetByID(c.Request().Context(), tradeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if t.TenantID != tid || t.UserID != uid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}
	var body JournalUpsertInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	entry := &journal_entry.JournalEntry{
		ID:          uuid.Must(uuid.NewV7()),
		TradeID:     tradeID,
		TenantID:    tid,
		UserID:      uid,
		Strategy:    body.Strategy,
		EntryReason: body.EntryReason,
		ExitReason:  body.ExitReason,
		Emotion:     body.Emotion,
		Mistake:     body.Mistake,
		Notes:       body.Notes,
		Metadata:    map[string]any{},
	}
	if err := h.store.JournalEntries.Upsert(c.Request().Context(), entry); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, entry)
}

// ── Tags ───────────────────────────────────────────────────────────────

func (h *Handler) ListTags(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tags, err := h.store.Tags.ListByTenant(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"tags": tags})
}

type TagCreateInput struct {
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

func (h *Handler) CreateTag(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body TagCreateInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.Name == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "name required"})))
	}
	tag := &tag.Tag{ID: uuid.Must(uuid.NewV7()), TenantID: tid, Name: body.Name, Color: body.Color, Owner: tag.OwnerUser, Metadata: map[string]any{}}
	_ = uid
	if err := h.store.Tags.Create(c.Request().Context(), tag); err != nil {
		if store.IsUniqueViolation(err) {
			return c.JSON(http.StatusConflict, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "duplicate tag name"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, tag)
}

func (h *Handler) DeleteTag(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	t, err := h.store.Tags.GetByID(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.NoContent(http.StatusNoContent)
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if t.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}
	if err := h.store.Tags.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

type TradeTagsSetInput struct {
	TagIDs []string `json:"tag_ids"`
}

func (h *Handler) SetTradeTags(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tradeID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	t, err := h.store.Trades.GetByID(c.Request().Context(), tradeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if t.TenantID != tid || t.UserID != uid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}
	var body TradeTagsSetInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	tagIDs := make([]uuid.UUID, 0, len(body.TagIDs))
	for _, s := range body.TagIDs {
		id, err := uuid.FromString(s)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid tag id"})))
		}
		tagIDs = append(tagIDs, id)
	}
	if err := h.store.TradeTags.SetTagsForTrade(c.Request().Context(), tradeID, tagIDs); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"tag_ids": body.TagIDs})
}

func (h *Handler) ListTradeTags(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tradeID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	t, err := h.store.Trades.GetByID(c.Request().Context(), tradeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if t.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}
	tagIDs, err := h.store.TradeTags.ListTagsForTrade(c.Request().Context(), tradeID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	out := make([]string, len(tagIDs))
	for i, id := range tagIDs {
		out[i] = id.String()
	}
	return c.JSON(http.StatusOK, map[string]any{"tag_ids": out})
}