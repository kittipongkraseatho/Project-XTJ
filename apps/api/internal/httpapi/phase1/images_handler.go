package phase1http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/trade_image"
	"github.com/trading-journal/api/internal/integration/cloudinary"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// allowed image MIME types (Phase 1)
var allowedImageMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// AttachImage — POST /v1/trades/:id/images (multipart, field = "image")
//
// Flow (spec §16, BE-mediated signed upload simplified for Phase 1):
//  1. Validate trade belongs to tenant
//  2. Validate MIME + size (≤ 5 MB Phase 1 limit)
//  3. Stream to Cloudinary with folder = trading-journal/tenants/{tid}/trades/{tradeId}
//  4. INSERT trade_images row
//  5. Return { public_id, secure_url, thumbnail_url }
func (h *Handler) AttachImage(c echo.Context) error {
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

	fileHeader, err := c.FormFile("image")
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err).WithDetails(map[string]any{"reason": "missing image"})))
	}
	if !allowedImageMimes[fileHeader.Header.Get("Content-Type")] {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "unsupported mime"})))
	}
	const maxImageBytes = 5 * 1024 * 1024
	if fileHeader.Size > maxImageBytes {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "image too large (max 5MB)"})))
	}
	src, err := fileHeader.Open()
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	defer src.Close()

	buf, err := io.ReadAll(src)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}

	folder := "tenants/" + tid.String() + "/trades/" + tradeID.String()
	asset, err := h.cloudinary.UploadImage(c.Request().Context(), folder, fileHeader.Filename, byteReaderImage(buf))
	if err != nil {
		h.log.Warn("cloudinary upload failed", slog.String("error", err.Error()))
		return c.JSON(http.StatusBadGateway, observability.FromError(
			observability.ErrImportFailed.WithCause(err)))
	}

	img := &trade_image.TradeImage{
		ID:                 uuid.Must(uuid.NewV7()),
		TenantID:           tid,
		TradeID:            tradeID,
		UserID:             uid,
		CloudinaryPublicID: asset.PublicID,
		CloudinaryAssetID:  strPtrP1(asset.AssetID),
		ResourceType:       asset.ResourceType,
		Format:             asset.Format,
		Width:              intPtrP1(asset.Width),
		Height:             intPtrP1(asset.Height),
		Bytes:              asset.Bytes,
		SecureURL:          asset.SecureURL,
		Folder:             strPtrP1(asset.Folder),
		Version:            int64PtrP1(asset.Version),
		Status:             trade_image.StatusReady,
	}
	if err := h.store.TradeImages.Insert(c.Request().Context(), img); err != nil {
		// best-effort delete cloudinary asset if DB insert fails
		_ = h.cloudinary.DeleteImage(c.Request().Context(), asset.PublicID)
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}

	_ = h.store.Audit.Insert(c.Request().Context(), store.AuditEntry{
		ActorUserID: &uid, ActorTenantID: &tid,
		Action: "trade.image_attached",
		ResourceType: strPtrP1("trade_image"), ResourceID: &img.ID,
		Metadata: map[string]any{"public_id": asset.PublicID, "trade_id": tradeID.String()},
	})

	return c.JSON(http.StatusCreated, map[string]any{
		"id":             img.ID,
		"public_id":      asset.PublicID,
		"secure_url":     asset.SecureURL,
		"thumbnail_url":  h.cloudinary.GetThumbnailURL(asset.PublicID),
		"format":         asset.Format,
		"width":          asset.Width,
		"height":         asset.Height,
		"bytes":          asset.Bytes,
	})
}

// ListImages — GET /v1/trades/:id/images
func (h *Handler) ListImages(c echo.Context) error {
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
	images, err := h.store.TradeImages.ListByTrade(c.Request().Context(), tradeID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	out := make([]map[string]any, len(images))
	for i, img := range images {
		out[i] = map[string]any{
			"id":            img.ID,
			"public_id":     img.CloudinaryPublicID,
			"secure_url":    img.SecureURL,
			"thumbnail_url": h.cloudinary.GetThumbnailURL(img.CloudinaryPublicID),
			"format":        img.Format,
			"width":         ptrIntToInt(img.Width),
			"height":        ptrIntToInt(img.Height),
			"bytes":         img.Bytes,
			"created_at":    img.CreatedAt,
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"images": out})
}

// ── tiny helpers ──────────────────────────────────────────────────────

func byteReaderImage(b []byte) *bytesReaderImage { return &bytesReaderImage{b: b} }

type bytesReaderImage struct {
	b []byte
	i int
}

func (r *bytesReaderImage) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, ioEOFImage
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

var ioEOFImage = errEOFImage{}

type errEOFImage struct{}

func (errEOFImage) Error() string { return "EOF" }

func strPtrP1(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
func intPtrP1(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
func int64PtrP1(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}
func ptrIntToInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

// ensure cloudinary import is used at least once
var _ = cloudinary.New