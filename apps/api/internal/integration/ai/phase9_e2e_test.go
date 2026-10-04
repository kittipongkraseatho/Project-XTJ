//go:build integration

// Package ai_test — Phase 9 E2E integration test (recap + chat flow).
//
// Boots a real Postgres+pgvector via testcontainers-go, runs migrations,
// then exercises the recap + chat lifecycle via HTTP:
//
//   1. POST /v1/auth/register           → JWT access token
//   2. POST /v1/accounts                → create a trading account
//   3. POST /v1/accounts/:id/recaps     → 202 (pending)
//   4. GET  /v1/recaps/:id              → status=success, content non-empty
//   5. POST /v1/chat/threads            → 201
//   6. POST /v1/chat/threads/:id/messages → 202 (pending user msg)
//   7. GET  /v1/chat/threads/:id/messages → assistant reply present
//   8. GET  /v1/ai/usage                → recap_requests + chat_requests bumped
//
// Skipped unless INTEGRATION_TESTS=on + Docker available; runs in CI.
package ai_test

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
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/trading-journal/api/internal/config"
	"github.com/trading-journal/api/internal/server"
)

func skipIfDisabled(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TESTS") != "on" {
		t.Skip("set INTEGRATION_TESTS=on to run full-stack integration tests (requires Docker)")
	}
	if _, err := testcontainers.NewDockerProvider(); err != nil {
		t.Skipf("Docker not available: %v", err)
	}
}

func TestPhase9_E2E_RecapThenChat(t *testing.T) {
	skipIfDisabled(t)
	ctx := context.Background()

	// 1. Boot pgvector Postgres.
	pgC, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage("pgvector/pgvector:pg16"),
		tcpostgres.WithDatabase("tj_test"),
		tcpostgres.WithUsername("tj"),
		tcpostgres.WithPassword("devpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		t.Fatalf("start pgvector: %v", err)
	}
	defer pgC.Terminate(ctx)
	pgHost, err := pgC.Host(ctx)
	if err != nil {
		t.Fatalf("pg host: %v", err)
	}
	pgPort, err := pgC.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("pg port: %v", err)
	}
	pgURL := fmt.Sprintf("postgres://tj:devpass@%s:%s/tj_test?sslmode=disable", pgHost, pgPort.Port())

	// 2. Configure env for server.New.
	t.Setenv("DATABASE_URL", pgURL)
	t.Setenv("REDIS_URL", "redis://localhost:6379/0") // Phase 5.1 redis isn't required for AI flow; mock provider used
	t.Setenv("JWT_SECRET_PATH", "")                  // auto-gen in keys/
	t.Setenv("AI_PROVIDER", "stub")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("EMBED_PROVIDER", "stub")
	t.Setenv("WEBHOOK_SIGNING_SECRET", "test-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.DatabaseURL = pgURL

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	e, cleanup, err := server.New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer cleanup()
	httpSrv := httptest.NewServer(e)
	defer httpSrv.Close()

	// 3. Register a user.
	email := fmt.Sprintf("phase9+%d@example.com", time.Now().UnixNano())
	password := "SuperSecret123!"
	body, _ := json.Marshal(map[string]any{
		"email": email, "password": password, "display_name": "Phase9",
	})
	resp, err := http.Post(httpSrv.URL+"/v1/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status %d", resp.StatusCode)
	}
	var reg struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()
	if reg.AccessToken == "" {
		t.Fatalf("register: no access_token")
	}
	authed := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+reg.AccessToken)
	}

	// 4. Create trading account.
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/accounts", strings.NewReader(`{
		"broker": "mock",
		"broker_account_id": "demo-1",
		"display_name": "Demo Account",
		"currency": "USD",
		"kind": "demo"
	}`))
	req.Header.Set("Content-Type", "application/json")
	authed(req)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create account status %d", resp.StatusCode)
	}
	var acc struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&acc)
	resp.Body.Close()
	accountID := acc.ID
	if accountID == "" {
		t.Fatalf("account id missing")
	}

	// 5. Kick recap.
	req, _ = http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/accounts/"+accountID+"/recaps", nil)
	authed(req)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("recap kick: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("recap kick status %d", resp.StatusCode)
	}
	var rc struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&rc)
	resp.Body.Close()
	recapID := rc.ID
	waitForRecap(t, httpSrv, recapID, reg.AccessToken)

	// 6. Create chat thread.
	req, _ = http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/chat/threads",
		strings.NewReader(fmt.Sprintf(`{"account_id":"%s","title":"my chat"}`, accountID)))
	req.Header.Set("Content-Type", "application/json")
	authed(req)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create thread status %d", resp.StatusCode)
	}
	var th struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&th)
	resp.Body.Close()
	threadID := th.ID

	// 7. Send chat message.
	req, _ = http.NewRequest(http.MethodPost,
		httpSrv.URL+"/v1/chat/threads/"+threadID+"/messages",
		strings.NewReader(`{"content":"What was my win rate?"}`))
	req.Header.Set("Content-Type", "application/json")
	authed(req)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("send message status %d", resp.StatusCode)
	}
	resp.Body.Close()
	waitForChatReply(t, httpSrv, threadID, reg.AccessToken)

	// 8. Verify usage counters bumped.
	req, _ = http.NewRequest(http.MethodGet, httpSrv.URL+"/v1/ai/usage", nil)
	authed(req)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("usage status %d", resp.StatusCode)
	}
	var u struct {
		Requests      int `json:"requests"`
		RecapRequests int `json:"recap_requests"`
		ChatRequests  int `json:"chat_requests"`
		DailyBudget   int `json:"daily_budget"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&u)
	resp.Body.Close()
	if u.RecapRequests < 1 {
		t.Fatalf("expected recap_requests ≥ 1, got %d", u.RecapRequests)
	}
	if u.ChatRequests < 1 {
		t.Fatalf("expected chat_requests ≥ 1, got %d", u.ChatRequests)
	}
}

// ── helpers ──────────────────────────────────────────────────────────

func waitForRecap(t *testing.T, srv *httptest.Server, recapID, token string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/recaps/"+recapID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("poll recap: %v", err)
		}
		var rc struct {
			Status  string  `json:"status"`
			Content *string `json:"content"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&rc)
		resp.Body.Close()
		if rc.Status == "success" {
			if rc.Content == nil || *rc.Content == "" {
				t.Fatalf("recap success but content empty")
			}
			return
		}
		if rc.Status == "failed" {
			t.Fatalf("recap failed")
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("recap did not complete in 60s")
}

func waitForChatReply(t *testing.T, srv *httptest.Server, threadID, token string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet,
			srv.URL+"/v1/chat/threads/"+threadID+"/messages", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("poll messages: %v", err)
		}
		var ms []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&ms)
		resp.Body.Close()
		for _, m := range ms {
			if m.Role == "assistant" && m.Content != "" {
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("chat reply did not arrive in 60s")
}