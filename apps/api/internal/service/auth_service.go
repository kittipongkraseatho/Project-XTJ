// Package service contains use-case orchestration. The handler (httpapi) layer
// is thin and delegates to these methods.
//
// All services accept context.Context and propagate it to the store layer.
// Tenant context is set by the auth middleware before handler dispatch.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/http"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/config"
	"github.com/trading-journal/api/internal/domain/membership"
	"github.com/trading-journal/api/internal/domain/plan"
	"github.com/trading-journal/api/internal/domain/subscription"
	"github.com/trading-journal/api/internal/domain/tenant"
	"github.com/trading-journal/api/internal/domain/user"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// AuthService handles all email/password + session business logic.
type AuthService struct {
	cfg   *config.Config
	store *store.Store
	jwt   *auth.JWTService
	log   *slog.Logger
}

func NewAuthService(cfg *config.Config, st *store.Store, jwt *auth.JWTService, log *slog.Logger) *AuthService {
	return &AuthService{cfg: cfg, store: st, jwt: jwt, log: log}
}

// ── DTOs returned to handlers ────────────────────────────────────────

type AuthUser struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

type AuthTenant struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

type AuthPlan struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Currency  string    `json:"currency"`
	PriceCents int64    `json:"price_cents"`
	Interval  string    `json:"interval"`
}

type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"` // always "Bearer"
	ExpiresIn    int64  `json:"expires_in"` // seconds until access_token expires
}

type RegisterResult struct {
	User   AuthUser   `json:"user"`
	Tenant AuthTenant `json:"tenant"`
	Plan   AuthPlan   `json:"plan"`
	Tokens AuthTokens `json:"tokens"`
}

type LoginResult = RegisterResult

type RefreshResult struct {
	Tokens AuthTokens `json:"tokens"`
}

type MeResult struct {
	User   AuthUser   `json:"user"`
	Tenant AuthTenant `json:"tenant"`
	Plan   AuthPlan   `json:"plan"`
	Role   string     `json:"role"`
}

// ── Context keys propagated through service calls ─────────────────────

type CallCtx struct {
	RequestID string
	UserAgent string
	IP        *netip.Addr
}

// ── Register ─────────────────────────────────────────────────────────

type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput, call CallCtx) (*RegisterResult, error) {
	if err := auth.IsStrongEnough(in.Password); err != nil {
		return nil, observability.ErrPasswordTooWeak.WithDetails(map[string]any{"reason": err.Error()}).WithCause(err)
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	if existing, _ := s.store.Users.GetByEmail(ctx, in.Email); existing != nil {
		return nil, observability.ErrEmailTaken
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// IDs
	userID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("user uuid: %w", err)
	}
	tenantID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("tenant uuid: %w", err)
	}
	memID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("membership uuid: %w", err)
	}
	subID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("subscription uuid: %w", err)
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("session uuid: %w", err)
	}

	// Free plan (seeded)
	freePlan, err := s.store.Plans.GetByCode(ctx, plan.CodeFree)
	if err != nil {
		return nil, fmt.Errorf("lookup free plan (did you apply migrations?): %w", err)
	}

	// Slug
	slug := slugify(in.DisplayName)
	if slug == "" {
		slug = "workspace-" + tenantID.String()[:8]
	}

	u := &user.User{
		ID:           userID,
		Email:        in.Email,
		PasswordHash: hash,
		DisplayName:  in.DisplayName,
		Status:       user.StatusActive,
		Metadata:     map[string]any{},
	}
	t := &tenant.Tenant{
		ID:       tenantID,
		Name:     in.DisplayName + " workspace",
		Slug:     slug,
		Status:   tenant.StatusActive,
		Metadata: map[string]any{},
	}
	m := &membership.Membership{
		ID:       memID,
		TenantID: tenantID,
		UserID:   userID,
		Role:     membership.RoleOwner,
		Status:   membership.StatusActive,
	}
	now := time.Now().UTC()
	sub := &subscription.Subscription{
		ID:                 subID,
		TenantID:           tenantID,
		PlanID:             freePlan.ID,
		Status:             subscription.StatusActive,
		StartedAt:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
		Metadata:           map[string]any{},
	}

	// Issue tokens (refresh session also created inside same tx)
	refresh, refreshHash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	rs := &store.RefreshSession{
		ID:        sessionID,
		UserID:    userID,
		TenantID:  tenantID,
		TokenHash: refreshHash,
		Status:    "active",
		UserAgent: stringPtrOrNil(call.UserAgent),
		IP:        call.IP,
		ExpiresAt: now.Add(s.cfg.RefreshTokenTTL),
	}

	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.store.Users.WithTx(tx).Create(ctx, u); err != nil {
			return err
		}
		if err := s.store.Tenants.WithTx(tx).Create(ctx, t); err != nil {
			return err
		}
		if err := s.store.Memberships.WithTx(tx).Create(ctx, m); err != nil {
			return err
		}
		if err := s.store.Subscriptions.WithTx(tx).Create(ctx, sub); err != nil {
			return err
		}
		if err := s.store.Refresh.WithTx(tx).Create(ctx, rs); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return nil, observability.ErrEmailTaken.WithCause(err)
		}
		return nil, fmt.Errorf("register tx: %w", err)
	}

	accessTok, expiresAt, err := s.jwt.Sign(auth.SignInput{
		UserID: userID, TenantID: tenantID, SessionID: sessionID, Role: string(membership.RoleOwner),
	})
	if err != nil {
		return nil, fmt.Errorf("sign access: %w", err)
	}

	// Audit (best-effort; not in tx — audit history is forgiving)
	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID:   &userID,
		ActorTenantID: &tenantID,
		Action:        "user.registered",
		ResourceType:  stringPtr("user"),
		ResourceID:    &userID,
		IP:            call.IP,
		UserAgent:     stringPtrOrNil(call.UserAgent),
		RequestID:     stringPtrOrNilUUID(call.RequestID),
	})
	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID:   &userID,
		ActorTenantID: &tenantID,
		Action:        "tenant.created",
		ResourceType:  stringPtr("tenant"),
		ResourceID:    &tenantID,
	})

	return &RegisterResult{
		User:   AuthUser{ID: userID, Email: in.Email, DisplayName: in.DisplayName},
		Tenant: AuthTenant{ID: tenantID, Name: t.Name, Slug: t.Slug},
		Plan: AuthPlan{
			ID: freePlan.ID, Code: string(freePlan.Code), Name: freePlan.Name,
			Currency: freePlan.Currency, PriceCents: freePlan.PriceCents, Interval: string(freePlan.Interval),
		},
		Tokens: AuthTokens{
			AccessToken:  accessTok,
			RefreshToken: refresh,
			TokenType:    "Bearer",
			ExpiresIn:    int64(time.Until(expiresAt).Seconds()),
		},
	}, nil
}

// ── Login ─────────────────────────────────────────────────────────────

type LoginInput struct {
	Email    string
	Password string
}

func (s *AuthService) Login(ctx context.Context, in LoginInput, call CallCtx) (*LoginResult, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	u, err := s.store.Users.GetByEmail(ctx, in.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Constant-time dummy verify to avoid timing leak
			_ = auth.VerifyPassword(in.Password, "$argon2id$v=19$m=65536,t=1,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
			return nil, observability.ErrInvalidCredentials
		}
		return nil, err
	}
	if err := auth.VerifyPassword(in.Password, u.PasswordHash); err != nil {
		_ = s.store.Audit.Insert(ctx, store.AuditEntry{
			ActorUserID: &u.ID, Action: "user.login_failed",
			ResourceType: stringPtr("user"), ResourceID: &u.ID,
			IP: call.IP, UserAgent: stringPtrOrNil(call.UserAgent),
			Metadata: map[string]any{"reason": "invalid_password"},
		})
		return nil, observability.ErrInvalidCredentials
	}
	if u.Status != user.StatusActive {
		return nil, observability.ErrUnauthorized
	}

	// Pick primary membership (Phase 0: any active membership)
	mems, err := s.store.Memberships.ListByUser(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	if len(mems) == 0 {
		return nil, observability.ErrMembershipNotFound
	}
	mem := mems[0]
	t, err := s.store.Tenants.GetByID(ctx, mem.TenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}
	sub, err := s.store.Subscriptions.GetActiveByTenant(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("active subscription: %w", err)
	}
	planRow, err := s.store.Plans.GetByID(ctx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	// Issue tokens
	sessionID, _ := uuid.NewV7()
	refresh, refreshHash, _ := auth.NewRefreshToken()
	rs := &store.RefreshSession{
		ID: sessionID, UserID: u.ID, TenantID: t.ID, TokenHash: refreshHash, Status: "active",
		UserAgent: stringPtrOrNil(call.UserAgent), IP: call.IP,
		ExpiresAt: time.Now().UTC().Add(s.cfg.RefreshTokenTTL),
	}
	if err := s.store.Refresh.Create(ctx, rs); err != nil {
		return nil, fmt.Errorf("create refresh session: %w", err)
	}
	accessTok, expiresAt, err := s.jwt.Sign(auth.SignInput{
		UserID: u.ID, TenantID: t.ID, SessionID: sessionID, Role: string(mem.Role),
	})
	if err != nil {
		return nil, fmt.Errorf("sign access: %w", err)
	}

	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &u.ID, ActorTenantID: &t.ID, Action: "user.login",
		ResourceType: stringPtr("user"), ResourceID: &u.ID,
		IP: call.IP, UserAgent: stringPtrOrNil(call.UserAgent),
	})

	return &LoginResult{
		User:   AuthUser{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName},
		Tenant: AuthTenant{ID: t.ID, Name: t.Name, Slug: t.Slug},
		Plan:   AuthPlan{ID: planRow.ID, Code: string(planRow.Code), Name: planRow.Name, Currency: planRow.Currency, PriceCents: planRow.PriceCents, Interval: string(planRow.Interval)},
		Tokens: AuthTokens{
			AccessToken: accessTok, RefreshToken: refresh, TokenType: "Bearer",
			ExpiresIn: int64(time.Until(expiresAt).Seconds()),
		},
	}, nil
}

// ── Refresh ──────────────────────────────────────────────────────────

type RefreshInput struct {
	RefreshToken string
}

func (s *AuthService) Refresh(ctx context.Context, in RefreshInput, call CallCtx) (*RefreshResult, error) {
	hash := auth.HashRefreshToken(in.RefreshToken)
	rs, err := s.store.Refresh.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, observability.ErrRefreshTokenInvalid
		}
		return nil, err
	}
	if rs.Status != "active" {
		if rs.Status == "revoked" {
			// REUSE DETECTED — revoke ALL active sessions for this user
			_, _ = s.store.Refresh.RevokeAllForUser(ctx, rs.UserID)
			_ = s.store.Audit.Insert(ctx, store.AuditEntry{
				ActorUserID: &rs.UserID, Action: "user.refresh_reuse_detected",
				ResourceType: stringPtr("user"), ResourceID: &rs.UserID,
				IP: call.IP, UserAgent: stringPtrOrNil(call.UserAgent),
			})
			return nil, observability.ErrRefreshTokenReuse
		}
		if rs.Status == "expired" || rs.ExpiresAt.Before(time.Now()) {
			return nil, observability.ErrRefreshTokenExpired
		}
		return nil, observability.ErrRefreshTokenInvalid
	}

	// Look up user + membership role for the new access token
	u, err := s.store.Users.GetByID(ctx, rs.UserID)
	if err != nil {
		return nil, fmt.Errorf("user for refresh: %w", err)
	}
	mem, err := s.store.Memberships.GetByUserAndTenant(ctx, rs.UserID, rs.TenantID)
	if err != nil {
		return nil, fmt.Errorf("membership: %w", err)
	}

	// Rotate: new session
	newSessionID, _ := uuid.NewV7()
	newRefresh, newHash, _ := auth.NewRefreshToken()
	newRs := &store.RefreshSession{
		ID: newSessionID, UserID: rs.UserID, TenantID: rs.TenantID,
		TokenHash: newHash, Status: "active",
		UserAgent: stringPtrOrNil(call.UserAgent), IP: call.IP,
		ExpiresAt: time.Now().UTC().Add(s.cfg.RefreshTokenTTL),
	}

	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.store.Refresh.WithTx(tx).Create(ctx, newRs); err != nil {
			return err
		}
		if err := s.store.Refresh.WithTx(tx).Rotate(ctx, rs.ID, newSessionID); err != nil {
			return err
		}
		if err := s.store.Refresh.WithTx(tx).Touch(ctx, newSessionID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rotate tx: %w", err)
	}

	accessTok, expiresAt, err := s.jwt.Sign(auth.SignInput{
		UserID: u.ID, TenantID: rs.TenantID, SessionID: newSessionID, Role: string(mem.Role),
	})
	if err != nil {
		return nil, fmt.Errorf("sign access: %w", err)
	}

	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &u.ID, ActorTenantID: &rs.TenantID, Action: "user.refresh",
		ResourceType: stringPtr("refresh_session"), ResourceID: &newSessionID,
	})

	return &RefreshResult{
		Tokens: AuthTokens{
			AccessToken: accessTok, RefreshToken: newRefresh, TokenType: "Bearer",
			ExpiresIn: int64(time.Until(expiresAt).Seconds()),
		},
	}, nil
}

// ── Logout ───────────────────────────────────────────────────────────

type LogoutInput struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
}

func (s *AuthService) Logout(ctx context.Context, in LogoutInput, call CallCtx) error {
	if err := s.store.Refresh.Revoke(ctx, in.SessionID); err != nil {
		return fmt.Errorf("revoke refresh: %w", err)
	}
	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &in.UserID, Action: "user.logout",
		ResourceType: stringPtr("refresh_session"), ResourceID: &in.SessionID,
		IP: call.IP, UserAgent: stringPtrOrNil(call.UserAgent),
	})
	return nil
}

// ── Me ───────────────────────────────────────────────────────────────

type MeInput struct {
	UserID   uuid.UUID
	TenantID uuid.UUID
	Role     string
}

func (s *AuthService) Me(ctx context.Context, in MeInput) (*MeResult, error) {
	u, err := s.store.Users.GetByID(ctx, in.UserID)
	if err != nil {
		return nil, fmt.Errorf("user: %w", err)
	}
	t, err := s.store.Tenants.GetByID(ctx, in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}
	sub, err := s.store.Subscriptions.GetActiveByTenant(ctx, in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("active sub: %w", err)
	}
	planRow, err := s.store.Plans.GetByID(ctx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	return &MeResult{
		User:   AuthUser{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName},
		Tenant: AuthTenant{ID: t.ID, Name: t.Name, Slug: t.Slug},
		Plan:   AuthPlan{ID: planRow.ID, Code: string(planRow.Code), Name: planRow.Name, Currency: planRow.Currency, PriceCents: planRow.PriceCents, Interval: string(planRow.Interval)},
		Role:   in.Role,
	}, nil
}

// ── helpers ──────────────────────────────────────────────────────────

func stringPtr(s string) *string { return &s }
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func stringPtrOrNilUUID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	u, err := uuid.FromString(s)
	if err != nil {
		return nil
	}
	return &u
}

// slugify makes a tenant slug from a display name.
// Phase 0: simple lowercase + hyphenate + length cap + UUID suffix for uniqueness.
func slugify(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if !prevHyphen && b.Len() > 0 {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
		if b.Len() >= 32 {
			break
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		return ""
	}
	return s
}

// status returns the HTTP status for a service error.
func status(err error) int {
	return observability.HTTPStatus(err)
}

// suppress unused warning if helpers are dropped during refactors.
var _ = http.StatusOK
var _ = errors.New
