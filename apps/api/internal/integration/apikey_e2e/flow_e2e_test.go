// Package apikey_e2e_test — full-stack integration test for the public API.
//
// Boots a real Postgres + Redis via testcontainers-go, runs migrations,
// then exercises the full API-key lifecycle via HTTP:
//
//  1. POST /v1/auth/register           → JWT cookie/access tokens
//  2. GET  /v1/me                      → confirms tenant
//  3. POST /v1/api-keys                → mints a key, returns plaintext
//  4. GET  /v1/public/accounts         (no key) → 401
//  5. GET  /v1/public/accounts         (with key) → 200
//  6. POST /v1/api-keys (only read:accounts) → call read:trades → 403
//  7. burn burst → 11th call → 429 + Retry-After
//  8. DELETE /v1/api-keys/:id          → 204
//  9. reuse plaintext                      → 401 (revoked)
//  10. GET /v1/api-keys                 → confirm revoked status
//
// Gated by `INTEGRATION_TESTS=on` (default off — skipped without Docker).
package apikey_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/trading-journal/api/internal/config"
	"github.com/trading-journal/api/internal/server"
)

// migrationsDir locates the project's migrations/ directory at runtime.
//
// We can't use //go:embed because embed doesn't allow `..` paths. Instead
// we walk up from this test file's source location to the module root
// and load the *.up.sql files from disk.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "migrations")
		if entries, err := os.ReadDir(cand); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".up.sql") {
					return cand
				}
			}
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate migrations/ from test file")
	return ""
}

func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TESTS") != "on" {
		t.Skip("set INTEGRATION_TESTS=on to run full-stack integration tests (requires Docker)")
	}
	if _, err := testcontainers.NewDockerProvider(); err != nil {
		t.Skipf("Docker not available: %v", err)
	}
}

// ── Test harness ─────────────────────────────────────────────────────

type harness struct {
	t       *testing.T
	pool    *pgxpool.Pool
	rdb     *redis.Client
	server  *httptest.Server
	cleanup []func()
}

func (h *harness) close() {
	for i := len(h.cleanup) - 1; i >= 0; i-- {
		h.cleanup[i]()
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	h := &harness{t: t}

	// ── Postgres ────────────────────────────────────────────────────────
	pg, err := tcpostgres.Run(ctx, "pgvector/pgvector:pg16",
		tcpostgres.WithDatabase("tj_test"),
		tcpostgres.WithUsername("tj"),
		tcpostgres.WithPassword("devpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	h.cleanup = append(h.cleanup, func() { _ = pg.Terminate(context.Background()) })

	pgURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres URL: %v", err)
	}

	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	h.pool = pool
	h.cleanup = append(h.cleanup, pool.Close)

	if err := applyMigrations(t, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	// ── Redis ───────────────────────────────────────────────────────────
	rdbContainer, err := tcredis.Run(ctx, "redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}
	h.cleanup = append(h.cleanup, func() { _ = rdbContainer.Terminate(context.Background()) })

	redisURL, err := rdbContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis URL: %v", err)
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis URL: %v", err)
	}
	h.rdb = redis.NewClient(opt)
	h.cleanup = append(h.cleanup, func() { _ = h.rdb.Close() })

	// ── Server ─────────────────────────────────────────────────────────
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "ed25519.pem")

	cfg := &config.Config{
		Env:                     "test",
		LogLevel:                "warn",
		APIPort:                 0,
		DatabaseURL:             pgURL,
		DBMaxConns:              5,
		RedisURL:                redisURL,
		CORSAllowedOrigins:      []string{"http://localhost:3000"},
		JWTKeyPath:              keyPath,
		JWTIssuer:               "trading-journal-test",
		JWTAudience:             "trading-journal-web-test",
		AccessTokenTTL:          5 * time.Minute,
		RefreshTokenTTL:         time.Hour,
		CloudinaryFolder:        "trading-journal-test",
		SyncPollIntervalSeconds: 30,
		SMTPPort:                587,
		SMTPFrom:                "test@example.com",
		MetaAPIRegion:           "ap-southeast",
		APIPublicRatePerMinute:  60,
		APIPublicRateBurst:      10,
		RateLimitBackend:        "redis",
		RateLimitKeyPrefix:      "trjtest:rl:",
		RateLimitTTLSeconds:     60,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	echoSrv, cleanupFn, err := server.New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	h.cleanup = append(h.cleanup, cleanupFn)

	ts := httptest.NewServer(echoSrv)
	h.server = ts
	h.cleanup = append(h.cleanup, ts.Close)
	return h
}

// applyMigrations runs every 000N_*.up.sql file in lexical order.
func applyMigrations(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(context.Background(), string(raw)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// ── HTTP helpers ─────────────────────────────────────────────────────

type httpResp struct {
	status int
	body   map[string]any
	raw    []byte
	header http.Header
}

func (h *harness) do(t *testing.T, method, path string, body any, headers map[string]string) httpResp {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := httpResp{status: res.StatusCode, raw: raw, header: res.Header}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.body)
	}
	return out
}

func extractToken(t *testing.T, r httpResp) string {
	t.Helper()
	tok, ok := r.body["access_token"].(string)
	if !ok || tok == "" {
		if t, ok := r.body["tokens"].(map[string]any); ok {
			tok, _ = t["access_token"].(string)
		}
	}
	if tok == "" {
		t.Fatalf("no access_token in response: %s", string(r.raw))
	}
	return tok
}

func extractCookie(r httpResp, name string) string {
	for _, c := range r.header.Values("Set-Cookie") {
		if strings.HasPrefix(c, name+"=") {
			parts := strings.SplitN(c, "=", 2)
			if len(parts) == 2 {
				return strings.SplitN(parts[1], ";", 2)[0]
			}
		}
	}
	return ""
}

// ── Test ─────────────────────────────────────────────────────────────

func TestE2E_ApiKeyFullFlow(t *testing.T) {
	skipUnlessIntegration(t)
	h := newHarness(t)
	defer h.close()

	email := fmt.Sprintf("user-%d@example.com", time.Now().UnixNano())
	password := "supersecret123"

	// 1. Register
	reg := h.do(t, "POST", "/v1/auth/register", map[string]string{
		"email": email, "password": password, "display_name": "Tester",
	}, nil)
	if reg.status != http.StatusCreated && reg.status != http.StatusOK {
		t.Fatalf("register: status %d body %s", reg.status, string(reg.raw))
	}
	accessTok := extractToken(t, reg)
	refreshCookie := extractCookie(reg, "tj_refresh")
	if refreshCookie == "" {
		t.Fatal("no refresh cookie from register")
	}

	auth := map[string]string{"Authorization": "Bearer " + accessTok}

	// 2. /me
	me := h.do(t, "GET", "/v1/me", nil, auth)
	if me.status != http.StatusOK {
		t.Fatalf("/me: status %d body %s", me.status, string(me.raw))
	}

	// 3. Create API key (read-only scopes)
	create := h.do(t, "POST", "/v1/api-keys", map[string]any{
		"name":       "zapier",
		"scopes":     []string{"read:accounts", "read:trades"},
		"expires_in": "never",
	}, auth)
	if create.status != http.StatusCreated {
		t.Fatalf("create api key: status %d body %s", create.status, string(create.raw))
	}
	plaintext, _ := create.body["plaintext"].(string)
	if plaintext == "" {
		t.Fatalf("plaintext empty: %s", string(create.raw))
	}
	if !strings.HasPrefix(plaintext, "trj_pk_") {
		t.Fatalf("plaintext doesn't look like an api key: %s", plaintext)
	}

	// 4. /v1/public/accounts without auth
	pubUnauth := h.do(t, "GET", "/v1/public/accounts", nil, nil)
	if pubUnauth.status != http.StatusUnauthorized {
		t.Fatalf("public unauth want 401 got %d", pubUnauth.status)
	}

	// 5. /v1/public/accounts WITH the API key
	pubOK := h.do(t, "GET", "/v1/public/accounts", nil, map[string]string{
		"Authorization": "Bearer " + plaintext,
	})
	if pubOK.status != http.StatusOK {
		t.Fatalf("public with key want 200 got %d body %s", pubOK.status, string(pubOK.raw))
	}

	// 6. Scope guard. Create a key with only read:accounts, hit read:trades.
	create2 := h.do(t, "POST", "/v1/api-keys", map[string]any{
		"name":       "scoped",
		"scopes":     []string{"read:accounts"},
		"expires_in": "never",
	}, auth)
	if create2.status != http.StatusCreated {
		t.Fatalf("create2: %d", create2.status)
	}
	plain2, _ := create2.body["plaintext"].(string)
	tradesScope := h.do(t, "GET", "/v1/public/trades", nil, map[string]string{
		"Authorization": "Bearer " + plain2,
	})
	if tradesScope.status != http.StatusForbidden {
		t.Fatalf("scope mismatch want 403 got %d body %s", tradesScope.status, string(tradesScope.raw))
	}

	// 7. Burn burst. We set burst=10 in the harness → 11th must be 429.
	var lastStatus int
	var lastBody []byte
	for i := 0; i < 12; i++ {
		r := h.do(t, "GET", "/v1/public/accounts", nil, map[string]string{
			"Authorization": "Bearer " + plaintext,
		})
		lastStatus = r.status
		lastBody = r.raw
		if r.status == http.StatusTooManyRequests {
			if r.header.Get("Retry-After") == "" {
				t.Fatalf("429 missing Retry-After header")
			}
			break
		}
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("rate limit not triggered; last status %d body %s", lastStatus, lastBody)
	}

	// 8. List keys, find plaintext one, revoke.
	list := h.do(t, "GET", "/v1/api-keys", nil, auth)
	if list.status != http.StatusOK {
		t.Fatalf("list: %d", list.status)
	}
	keysRaw, _ := list.body["keys"].([]any)
	var keyID string
	for _, k := range keysRaw {
		m := k.(map[string]any)
		if strings.HasPrefix(m["prefix"].(string), plaintext[:16]) {
			keyID = m["id"].(string)
			break
		}
	}
	if keyID == "" {
		t.Fatalf("could not find key with matching prefix; keys=%v", keysRaw)
	}

	revoke := h.do(t, "DELETE", "/v1/api-keys/"+keyID, nil, auth)
	if revoke.status != http.StatusNoContent {
		t.Fatalf("revoke: status %d", revoke.status)
	}

	// 9. Reuse revoked key
	revReuse := h.do(t, "GET", "/v1/public/accounts", nil, map[string]string{
		"Authorization": "Bearer " + plaintext,
	})
	if revReuse.status != http.StatusUnauthorized {
		t.Fatalf("revoked reuse want 401 got %d body %s", revReuse.status, string(revReuse.raw))
	}
	errCode := ""
	if e, ok := revReuse.body["error"].(map[string]any); ok {
		errCode, _ = e["code"].(string)
	}
	if errCode != "API_KEY_REVOKED" {
		t.Fatalf("revoked reuse want API_KEY_REVOKED got %q", errCode)
	}

	// 10. List confirms revoked status
	list2 := h.do(t, "GET", "/v1/api-keys", nil, auth)
	if list2.status != http.StatusOK {
		t.Fatalf("list2: %d", list2.status)
	}
	for _, k := range list2.body["keys"].([]any) {
		m := k.(map[string]any)
		if m["id"] == keyID {
			if m["revoked_at"] == nil {
				t.Fatalf("expected revoked_at set, got %v", m)
			}
			return
		}
	}
	t.Fatalf("revoked key not found in list")
}
