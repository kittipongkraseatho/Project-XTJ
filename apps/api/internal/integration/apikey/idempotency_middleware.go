package apikey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/store"
)

// IdempotencyTTL is how long a successful POST response is replayed.
const IdempotencyTTL = 24 * time.Hour

// IdempotencyMaxKeyLen caps the Idempotency-Key header length (defensive).
const IdempotencyMaxKeyLen = 255

// IdempotencyMiddleware enforces Idempotency-Key on every POST /v1/public/*
// request and replays the cached response on subsequent identical requests.
//
// Semantics (Stripe-style):
//   - POST without header         → 400 IDEMPOTENCY_KEY_REQUIRED
//   - POST with non-UUID header   → 400 IDEMPOTENCY_KEY_INVALID
//   - Cache miss                  → call handler, persist (status, body) on success
//   - Cache hit + same hash       → return cached response (status + body)
//   - Cache hit + different hash  → 409 IDEMPOTENCY_KEY_CONFLICT
//   - Non-POST (PUT/PATCH/DELETE) → bypass; URL is the idempotency key
//
// IdempotencyStore is the storage contract the middleware depends on. Lets
// tests inject a fake without standing up a real Postgres.
type IdempotencyStore interface {
	Lookup(ctx context.Context, apiKeyID uuid.UUID, key string) (*store.CachedResponse, error)
	Store(ctx context.Context, apiKeyID uuid.UUID, key, hash string, status int, body []byte, ttl time.Duration) error
}

// Must be installed AFTER Middleware (so the API key + tenant are on ctx).
func IdempotencyMiddleware(repo IdempotencyStore, log *slog.Logger) echo.MiddlewareFunc {
	if log == nil {
		log = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Non-POST: URL is the idempotency key. Skip.
			if c.Request().Method != http.MethodPost {
				return next(c)
			}

			// 1. Pull the API key (set by Middleware).
			k := FromContext(c.Request().Context())
			if k == nil {
				// Defensive — should never happen since Middleware runs first.
				return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_REQUIRED", "no api key on context"))
			}

			// 2. Validate Idempotency-Key header.
			rawKey := strings.TrimSpace(c.Request().Header.Get("Idempotency-Key"))
			if rawKey == "" {
				return c.JSON(http.StatusBadRequest, errorEnvelope("IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required for POST"))
			}
			if len(rawKey) > IdempotencyMaxKeyLen {
				return c.JSON(http.StatusBadRequest, errorEnvelope("IDEMPOTENCY_KEY_TOO_LONG", "Idempotency-Key exceeds 255 chars"))
			}
			if _, err := uuid.FromString(rawKey); err != nil {
				return c.JSON(http.StatusBadRequest, errorEnvelope("IDEMPOTENCY_KEY_INVALID", "Idempotency-Key must be a UUID"))
			}

			// 3. Read + buffer the body so we can both hash it AND pass to the handler.
			bodyBytes, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_FAILED", "cannot read body"))
			}
			_ = c.Request().Body.Close()
			c.Request().Body = io.NopCloser(bytes.NewReader(bodyBytes))

			// 4. Hash the (normalized) body.
			requestHash := hashRequestBody(bodyBytes)

			// 5. Lookup cache.
			cached, err := repo.Lookup(c.Request().Context(), k.ID, rawKey)
			if err != nil {
				log.Warn("idempotency lookup failed",
					slog.String("err", err.Error()),
					slog.String("api_key_id", k.ID.String()))
				// Fail open — proceed to handler (no replay).
			} else if cached != nil {
				if cached.RequestHash != requestHash {
					return c.JSON(http.StatusConflict, errorEnvelope("IDEMPOTENCY_KEY_CONFLICT",
						"same key used with a different request body"))
				}
				// Replay: write cached status + body.
				c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				c.Response().Header().Set("Idempotent-Replay", "true")
				return c.Blob(cached.ResponseStatus, echo.MIMEApplicationJSON, cached.ResponseBody)
			}

			// 6. Wrap ResponseWriter to capture the body for caching.
			rec := &recordingResponse{ResponseWriter: c.Response().Writer, body: &bytes.Buffer{}}
			c.Response().Writer = rec

			if err := next(c); err != nil {
				return err
			}

			// 7. Persist on success (2xx) only. Don't cache 4xx/5xx.
			if rec.status >= 200 && rec.status < 300 {
				if err := repo.Store(c.Request().Context(), k.ID, rawKey, requestHash, rec.status, rec.body.Bytes(), IdempotencyTTL); err != nil {
					log.Warn("idempotency store failed",
						slog.String("err", err.Error()),
						slog.String("api_key_id", k.ID.String()))
				}
			}
			return nil
		}
	}
}

// hashRequestBody returns SHA256 of the JSON-normalized body. If the body
// isn't valid JSON we fall back to hashing the raw bytes (still deterministic
// per request, but less collision-safe).
func hashRequestBody(body []byte) string {
	var anyVal any
	if err := json.Unmarshal(body, &anyVal); err == nil {
		normalized, _ := json.Marshal(anyVal) // marshalling a parsed map sorts keys
		sum := sha256.Sum256(normalized)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// recordingResponse is an http.ResponseWriter that captures the body + status
// for caching. Delegates everything else to the underlying writer.
type recordingResponse struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
}

func (r *recordingResponse) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recordingResponse) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
