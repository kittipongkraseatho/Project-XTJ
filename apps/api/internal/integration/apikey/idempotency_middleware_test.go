package apikey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/store"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeIdempotencyStore struct {
	mu     sync.Mutex
	rows   map[string]*store.CachedResponse // key = "api_key_id|idempotency_key"
	store  *store.IdempotencyRepo
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{rows: map[string]*store.CachedResponse{}}
}

func (f *fakeIdempotencyStore) Lookup(ctx context.Context, apiKeyID uuid.UUID, key string) (*store.CachedResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.rows[apiKeyID.String()+"|"+key]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (f *fakeIdempotencyStore) Store(ctx context.Context, apiKeyID uuid.UUID, key, hash string, status int, body []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := body
	f.rows[apiKeyID.String()+"|"+key] = &store.CachedResponse{
		RequestHash:    hash,
		ResponseStatus: status,
		ResponseBody:   cp,
		ExpiresAt:      time.Now().Add(ttl),
	}
	return nil
}

func (f *fakeIdempotencyStore) SweepExpired(ctx context.Context) (int64, error) {
	return 0, nil
}

// echoHandlerStandalone is a simple echo for the middleware test (no real router).
func newRequest(method, path, key string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	return req
}

func withAPIKey(req *http.Request, k *api_key.APIKey) *http.Request {
	ctx := WithAPIKey(req.Context(), k)
	return req.WithContext(ctx)
}

// ── Tests ────────────────────────────────────────────────────────────

func TestIdempotencyMiddleware_BypassesNonPOST(t *testing.T) {
	called := false
	mw := IdempotencyMiddleware(nil, slog.Default())
	e := echo.New()
	req := newRequest(http.MethodPut, "/v1/public/trades", "", []byte(`{}`))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := mw(func(c echo.Context) error {
		called = true
		return c.NoContent(http.StatusOK)
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !called {
		t.Fatalf("expected handler to be called for PUT")
	}
}

func TestIdempotencyMiddleware_RequiredOnPOST(t *testing.T) {
	mw := IdempotencyMiddleware(nil, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.Must(uuid.NewV7())}
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", "", []byte(`{}`)), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	called := false
	handler := mw(func(c echo.Context) error {
		called = true
		return c.NoContent(http.StatusOK)
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if called {
		t.Fatalf("expected 400 before handler runs")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "IDEMPOTENCY_KEY_REQUIRED") {
		t.Fatalf("expected IDEMPOTENCY_KEY_REQUIRED in body: %q", body)
	}
}

func TestIdempotencyMiddleware_InvalidUUID(t *testing.T) {
	mw := IdempotencyMiddleware(nil, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7())}
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", "not-a-uuid", []byte(`{}`)), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := mw(func(c echo.Context) error {
		t.Fatalf("handler should not run")
		return nil
	})
	_ = handler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "IDEMPOTENCY_KEY_INVALID") {
		t.Fatalf("expected IDEMPOTENCY_KEY_INVALID")
	}
}

func TestIdempotencyMiddleware_TooLong(t *testing.T) {
	mw := IdempotencyMiddleware(nil, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7())}
	longKey := strings.Repeat("a", 300)
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", longKey, []byte(`{}`)), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := mw(func(c echo.Context) error {
		t.Fatalf("handler should not run")
		return nil
	})
	_ = handler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "IDEMPOTENCY_KEY_TOO_LONG") {
		t.Fatalf("expected IDEMPOTENCY_KEY_TOO_LONG")
	}
}

func TestIdempotencyMiddleware_CacheMissStoresOnSuccess(t *testing.T) {
	repo := newFakeIdempotencyStore()
	mw := IdempotencyMiddleware(repo, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.Must(uuid.NewV7())}
	idemKey := uuid.Must(uuid.NewV7()).String()
	body := []byte(`{"symbol":"EURUSD"}`)
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", idemKey, body), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	called := 0
	handler := mw(func(c echo.Context) error {
		called++
		return c.JSON(http.StatusCreated, map[string]any{"ok": true})
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected 1 call, got %d", called)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	// Cache should be populated.
	row, _ := repo.Lookup(context.Background(), k.ID, idemKey)
	if row == nil {
		t.Fatalf("expected cache row to exist")
	}
	if row.ResponseStatus != http.StatusCreated {
		t.Fatalf("expected status 201 in cache, got %d", row.ResponseStatus)
	}
}

func TestIdempotencyMiddleware_CacheHitReplays(t *testing.T) {
	repo := newFakeIdempotencyStore()
	mw := IdempotencyMiddleware(repo, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.Must(uuid.NewV7())}
	idemKey := uuid.Must(uuid.NewV7()).String()
	body := []byte(`{"symbol":"EURUSD"}`)
	// Seed cache with a known response.
	hash := hashRequestBody(body)
	if err := repo.Store(context.Background(), k.ID, idemKey, hash, 201, []byte(`{"id":"abc"}`), IdempotencyTTL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", idemKey, body), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	called := 0
	handler := mw(func(c echo.Context) error {
		called++
		return c.NoContent(http.StatusOK)
	})
	_ = handler(c)
	if called != 0 {
		t.Fatalf("expected handler NOT to run on cache hit, ran %d times", called)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 (replayed), got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"id":"abc"`) {
		t.Fatalf("expected cached body, got %q", rec.Body.String())
	}
	if rec.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("expected Idempotent-Replay header")
	}
}

func TestIdempotencyMiddleware_CacheHitDifferentHashConflict(t *testing.T) {
	repo := newFakeIdempotencyStore()
	mw := IdempotencyMiddleware(repo, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7())}
	idemKey := uuid.Must(uuid.NewV7()).String()
	// Seed with hash of a different body.
	differentHash := "deadbeef" + strings.Repeat("0", 56)
	if err := repo.Store(context.Background(), k.ID, idemKey, differentHash, 201, []byte(`{"id":"old"}`), IdempotencyTTL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := []byte(`{"symbol":"GBPUSD"}`)
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", idemKey, body), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	called := 0
	handler := mw(func(c echo.Context) error {
		called++
		return c.NoContent(http.StatusOK)
	})
	_ = handler(c)
	if called != 0 {
		t.Fatalf("handler should not run on conflict")
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "IDEMPOTENCY_KEY_CONFLICT") {
		t.Fatalf("expected IDEMPOTENCY_KEY_CONFLICT in body")
	}
}

func TestIdempotencyMiddleware_NotCachedOn5xx(t *testing.T) {
	repo := newFakeIdempotencyStore()
	mw := IdempotencyMiddleware(repo, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7())}
	idemKey := uuid.Must(uuid.NewV7()).String()
	body := []byte(`{}`)
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", idemKey, body), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := mw(func(c echo.Context) error {
		return c.JSON(http.StatusInternalServerError, map[string]any{"err": "boom"})
	})
	_ = handler(c)
	row, _ := repo.Lookup(context.Background(), k.ID, idemKey)
	if row != nil {
		t.Fatalf("expected NO cache row on 5xx, got one")
	}
}

func TestHashRequestBody_NormalizesJSONKeys(t *testing.T) {
	a := hashRequestBody([]byte(`{"b":1,"a":2}`))
	b := hashRequestBody([]byte(`{"a":2,"b":1}`))
	if a != b {
		t.Fatalf("expected normalized hash to be order-independent: %s vs %s", a, b)
	}
	c := hashRequestBody([]byte(`{"a":3,"b":1}`))
	if a == c {
		t.Fatalf("different values should hash differently")
	}
}

func TestHashRequestBody_InvalidJSONFallback(t *testing.T) {
	// Non-JSON body falls back to raw bytes.
	a := hashRequestBody([]byte(`not json`))
	b := hashRequestBody([]byte(`not json`))
	if a != b {
		t.Fatalf("expected same hash for same raw bytes")
	}
	c := hashRequestBody([]byte(`not json2`))
	if a == c {
		t.Fatalf("expected different hash for different raw bytes")
	}
}

func TestRecordingResponse_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rr := &recordingResponse{ResponseWriter: rec, body: &bytes.Buffer{}}
	rr.WriteHeader(201)
	if rr.status != 201 {
		t.Fatalf("expected status 201, got %d", rr.status)
	}
}

func TestRecordingResponse_WriteDefaults200(t *testing.T) {
	rec := httptest.NewRecorder()
	rr := &recordingResponse{ResponseWriter: rec, body: &bytes.Buffer{}}
	_, _ = rr.Write([]byte("hi"))
	if rr.status != http.StatusOK {
		t.Fatalf("expected default 200, got %d", rr.status)
	}
	if rr.body.String() != "hi" {
		t.Fatalf("expected body to be captured")
	}
}

func TestIdempotencyMiddleware_FailOpenOnLookupError(t *testing.T) {
	// Use a fake repo that returns an error on Lookup.
	repo := &errorFake{err: errors.New("db down")}
	mw := IdempotencyMiddleware(repo, slog.Default())
	e := echo.New()
	k := &api_key.APIKey{ID: uuid.Must(uuid.NewV7())}
	req := withAPIKey(newRequest(http.MethodPost, "/v1/public/trades", uuid.Must(uuid.NewV7()).String(), []byte(`{}`)), k)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	called := 0
	handler := mw(func(c echo.Context) error {
		called++
		return c.NoContent(http.StatusOK)
	})
	_ = handler(c)
	if called != 1 {
		t.Fatalf("expected handler to run on lookup error (fail-open), ran %d", called)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ── helpers ──────────────────────────────────────────────────────────

// errorFake always returns err from Lookup (used to test fail-open behavior).
type errorFake struct{ err error }

func (e *errorFake) Lookup(ctx context.Context, apiKeyID uuid.UUID, key string) (*store.CachedResponse, error) {
	return nil, e.err
}
func (e *errorFake) Store(ctx context.Context, apiKeyID uuid.UUID, key, hash string, status int, body []byte, ttl time.Duration) error {
	return e.err
}

var _ = fmt.Sprintf
var _ = json.Marshal
var _ = io.Discard
var _ = errors.New
