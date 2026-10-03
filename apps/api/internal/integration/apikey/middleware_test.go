package apikey_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/ratelimit"
	"github.com/trading-journal/api/internal/tenant"
)

// fakeService implements apikey.Service for middleware tests.
type fakeService struct {
	auth  func(ctx context.Context, bearer string) (*api_key.APIKey, error)
}

func (f *fakeService) Authenticate(ctx context.Context, bearer string) (*api_key.APIKey, error) {
	return f.auth(ctx, bearer)
}

func (f *fakeService) Generate(ctx context.Context, in apikey.GenerateInput) (*apikey.GenerateResult, error) {
	return nil, errors.New("not implemented in test")
}

func (f *fakeService) List(ctx context.Context, tenantID uuid.UUID) ([]*api_key.APIKey, error) {
	return nil, nil
}

func (f *fakeService) Get(ctx context.Context, id uuid.UUID) (*api_key.APIKey, error) {
	return nil, errors.New("not implemented in test")
}

func (f *fakeService) Revoke(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeService) Update(ctx context.Context, k *api_key.APIKey) error {
	return nil
}

func makeKey(t *testing.T, scopes ...api_key.Scope) *api_key.APIKey {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())
	strScopes := make([]string, 0, len(scopes))
	for _, s := range scopes {
		strScopes = append(strScopes, string(s))
	}
	return &api_key.APIKey{
		ID:        id,
		TenantID:  tenantID,
		Prefix:    "trj_pk_test",
		Scopes:    strScopes,
		IsActive:  true,
		CreatedAt: time.Now(),
	}
}

func doRequest(t *testing.T, mw echo.MiddlewareFunc, authz string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/v1/public/test", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := mw(func(c echo.Context) error {
		// Verify context plumbing.
		ctx := c.Request().Context()
		if tenant.TenantIDFromContextOrZero(ctx) == uuid.Nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"err": "tenant missing"})
		}
		if apikey.FromContext(ctx) == nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"err": "apikey missing"})
		}
		return c.NoContent(http.StatusOK)
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestMiddleware_MissingAuthHeader(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) { return nil, nil }}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_REQUIRED" {
		t.Fatalf("want API_KEY_REQUIRED, got %v", e["code"])
	}
}

func TestMiddleware_BearerBadFormat(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, apikey.ErrInvalidBearer
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "garbage")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_INVALID" {
		t.Fatalf("want API_KEY_INVALID, got %v", e["code"])
	}
}

func TestMiddleware_UnknownKey(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, apikey.ErrUnknownKey
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, _ := doRequest(t, mw, "Bearer trj_pk_unknown.aaa")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestMiddleware_RevokedKey(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, apikey.ErrRevoked
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "Bearer trj_pk_xxx.yyy")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_REVOKED" {
		t.Fatalf("want API_KEY_REVOKED, got %v", e["code"])
	}
}

func TestMiddleware_ExpiredKey(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, apikey.ErrExpired
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "Bearer trj_pk_xxx.yyy")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_EXPIRED" {
		t.Fatalf("want API_KEY_EXPIRED, got %v", e["code"])
	}
}

func TestMiddleware_GenericInternalError(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, errors.New("postgres exploded")
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "Bearer trj_pk_xxx.yyy")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "INTERNAL_ERROR" {
		t.Fatalf("want INTERNAL_ERROR, got %v", e["code"])
	}
}

func TestMiddleware_ScopeMissing(t *testing.T) {
	k := makeKey(t) // no scopes
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) { return k, nil }}
	mw := apikey.Middleware(svc, nil, api_key.ScopeReadAccounts)
	rec, body := doRequest(t, mw, "Bearer trj_pk_test.aaa")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_SCOPE_MISSING" {
		t.Fatalf("want API_KEY_SCOPE_MISSING, got %v", e["code"])
	}
}

func TestMiddleware_ScopePresent(t *testing.T) {
	k := makeKey(t, api_key.ScopeReadAccounts)
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) { return k, nil }}
	mw := apikey.Middleware(svc, nil, api_key.ScopeReadAccounts)
	rec, _ := doRequest(t, mw, "Bearer trj_pk_test.aaa")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestMiddleware_NoRequiredScope_RunsHandler(t *testing.T) {
	k := makeKey(t, api_key.ScopeReadTrades)
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) { return k, nil }}
	// required == "" → no scope check
	mw := apikey.Middleware(svc, nil, "")
	rec, _ := doRequest(t, mw, "Bearer trj_pk_test.aaa")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestMiddleware_RateLimited(t *testing.T) {
	k := makeKey(t)
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) { return k, nil }}
	bucket := ratelimit.NewMemoryKeyBucket(60, 1) // burst=1 → 2nd request must hit 429
	mw := apikey.Middleware(svc, bucket, "")
	// First call uses the burst token; second must be denied.
	rec1, _ := doRequest(t, mw, "Bearer trj_pk_test.aaa")
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request want 200, got %d", rec1.Code)
	}
	rec2, body := doRequest(t, mw, "Bearer trj_pk_test.aaa")
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request want 429, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Fatalf("want Retry-After header, got empty")
	}
	if e := body["error"].(map[string]any); e["code"] != "RATE_LIMITED" {
		t.Fatalf("want RATE_LIMITED, got %v", e["code"])
	}
}

func TestMiddleware_SuccessInjectsTenantAndKey(t *testing.T) {
	k := makeKey(t, api_key.ScopeReadAccounts)
	svc := &fakeService{auth: func(ctx context.Context, bearer string) (*api_key.APIKey, error) {
		// Verify the caller's ctx is the request ctx.
		if ctx == nil {
			return nil, errors.New("ctx nil")
		}
		return k, nil
	}}
	mw := apikey.Middleware(svc, nil, api_key.ScopeReadAccounts)
	rec, _ := doRequest(t, mw, "Bearer trj_pk_test.aaa")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	// The handler-level assertion in doRequest checks tenant + apikey on ctx.
}

func TestMiddleware_BadSecret(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, apikey.ErrBadSecret
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "Bearer trj_pk_xxx.aaa")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_INVALID" {
		t.Fatalf("want API_KEY_INVALID, got %v", e["code"])
	}
}

func TestMiddleware_Inactive(t *testing.T) {
	svc := &fakeService{auth: func(context.Context, string) (*api_key.APIKey, error) {
		return nil, apikey.ErrInactive
	}}
	mw := apikey.Middleware(svc, nil, "")
	rec, body := doRequest(t, mw, "Bearer trj_pk_xxx.aaa")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if e := body["error"].(map[string]any); e["code"] != "API_KEY_INACTIVE" {
		t.Fatalf("want API_KEY_INACTIVE, got %v", e["code"])
	}
}

func TestFromContext_Nil(t *testing.T) {
	if apikey.FromContext(context.Background()) != nil {
		t.Fatalf("want nil, got value")
	}
}

func TestWithAPIKey_RoundTrip(t *testing.T) {
	k := makeKey(t)
	ctx := apikey.WithAPIKey(context.Background(), k)
	if got := apikey.FromContext(ctx); got != k {
		t.Fatalf("round-trip mismatch")
	}
}