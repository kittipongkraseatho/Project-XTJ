package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
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

// Flow orchestrates the OAuth start + callback handlers.
//
// Start produces a state token, persists it (with PKCE verifier), and returns
// the authorize URL to redirect to. HandleCallback validates the state, exchanges
// the code for tokens, fetches user info, and links-or-creates a user, then
// returns our own JWT access + refresh pair for FE consumption.
type Flow struct {
	cfg      *config.Config
	store    *store.Store
	registry *Registry
	jwt      *auth.JWTService
	log      *slog.Logger
}

func NewFlow(cfg *config.Config, st *store.Store, reg *Registry, j *auth.JWTService, log *slog.Logger) *Flow {
	return &Flow{cfg: cfg, store: st, registry: reg, jwt: j, log: log}
}

// StartResult is what the start handler returns to redirect the user.
type StartResult struct {
	AuthorizeURL string
}

// Start generates state + PKCE verifier, persists in oauth_states,
// and returns the provider's authorize URL.
func (f *Flow) Start(ctx context.Context, provider store.IdentityProvider, ip, ua string) (*StartResult, error) {
	p, err := f.registry.Get(provider)
	if err != nil {
		return nil, observability.ErrOAuthProviderNotConfigured.WithCause(err)
	}
	state, err := NewStateToken()
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, fmt.Errorf("pkce: %w", err)
	}
	redirectURI := ""
	if provider == store.ProviderGoogle {
		redirectURI = f.cfg.OAuthGoogleRedirect
	}
	if provider == store.ProviderLine {
		redirectURI = f.cfg.OAuthLineRedirect
	}

	st := &store.OAuthState{
		StateToken:   state,
		Flow:         "login",
		Provider:     provider,
		RedirectURI:  &redirectURI,
		CodeVerifier: &verifier,
		IP:           strPtr(ip),
		UserAgent:    strPtr(ua),
		ExpiresAt:    time.Now().UTC().Add(10 * time.Minute),
	}
	if err := f.store.OAuthStates.Create(ctx, st); err != nil {
		return nil, fmt.Errorf("persist oauth state: %w", err)
	}
	return &StartResult{AuthorizeURL: p.AuthorizeURL(state, challenge, redirectURI)}, nil
}

// CallbackResult is the JWT pair returned after a successful OAuth login.
type CallbackResult struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int64
	RedirectTo   string // FE URL to redirect browser to, e.g., /auth/oauth-complete?access=...&refresh=...
}

// CallbackInput contains the raw callback params.
type CallbackInput struct {
	Provider store.IdentityProvider
	Code     string
	State    string
	IP       string
	UA       string
}

// HandleCallback validates the state, exchanges the code, fetches userinfo,
// links-or-creates the user, and issues our JWT pair.
func (f *Flow) HandleCallback(ctx context.Context, in CallbackInput) (*CallbackResult, error) {
	p, err := f.registry.Get(in.Provider)
	if err != nil {
		return nil, observability.ErrOAuthProviderNotConfigured.WithCause(err)
	}

	// 1. State lookup + validate
	st, err := f.store.OAuthStates.GetByToken(ctx, in.State)
	if err != nil {
		return nil, observability.ErrOAuthStateInvalid.WithCause(err)
	}
	if st.ConsumedAt != nil {
		return nil, observability.ErrOAuthStateInvalid
	}
	if st.ExpiresAt.Before(time.Now()) {
		return nil, observability.ErrOAuthStateInvalid
	}
	if st.Provider != in.Provider || st.RedirectURI == nil || st.CodeVerifier == nil {
		return nil, observability.ErrOAuthStateInvalid
	}
	// Mark consumed
	if err := f.store.OAuthStates.Consume(ctx, in.State); err != nil {
		return nil, fmt.Errorf("consume state: %w", err)
	}

	// 2. Exchange code → tokens
	tok, err := p.ExchangeCode(ctx, in.Code, *st.CodeVerifier, *st.RedirectURI)
	if err != nil {
		return nil, observability.ErrOAuthCodeExchange.WithCause(err)
	}

	// 3. Userinfo
	ui, err := p.FetchUserInfo(ctx, tok.AccessToken)
	if err != nil {
		return nil, observability.ErrOAuthUserInfo.WithCause(err)
	}
	if ui.Sub == "" {
		return nil, errors.New("provider userinfo missing sub")
	}

	// 4. Link-or-create
	var existingID *store.UserIdentity
	if id, err := f.store.UserIdentities.GetByProviderSub(ctx, in.Provider, ui.Sub); err == nil {
		existingID = id
	}

	if existingID != nil {
		// Returning user — issue tokens for their tenant
		return f.issueTokensForExisting(ctx, existingID, tok, in)
	}

	// No existing identity. Auto-link by verified email if possible.
	if ui.Email != "" && ui.EmailVerified {
		if u, err := f.store.Users.GetByEmail(ctx, ui.Email); err == nil {
			return f.linkAndIssue(ctx, p.Name(), ui, u, tok, in)
		}
	}

	// Create new user (tenant + membership + free subscription) + identity in one tx
	return f.createNewUserAndIssue(ctx, p.Name(), ui, in)
}

func (f *Flow) issueTokensForExisting(ctx context.Context, id *store.UserIdentity, tok TokenResponse, in CallbackInput) (*CallbackResult, error) {
	u, err := f.store.Users.GetByID(ctx, id.UserID)
	if err != nil {
		return nil, fmt.Errorf("identity user: %w", err)
	}
	mem, err := f.store.Memberships.GetByUserAndTenant(ctx, u.ID, id.TenantID)
	if err != nil {
		return nil, fmt.Errorf("identity membership: %w", err)
	}
	_ = f.store.UserIdentities.Touch(ctx, id.ID)
	return f.signTokens(ctx, u.ID, id.TenantID, mem.Role, in)
}

func (f *Flow) linkAndIssue(ctx context.Context, provider store.IdentityProvider, ui UserInfo, u *user.User, tok TokenResponse, in CallbackInput) (*CallbackResult, error) {
	// Pick the user's primary membership (Phase 0: any active one).
	mList, err := f.store.Memberships.ListByUser(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	if len(mList) == 0 {
		return nil, observability.ErrMembershipNotFound
	}
	mem := mList[0]

	id := &store.UserIdentity{
		ID: uuidMustV7(), UserID: u.ID, TenantID: mem.TenantID,
		Provider: provider, ProviderSub: ui.Sub,
		Email: strPtr(ui.Email), EmailVerified: ui.EmailVerified,
		DisplayName: strPtr(ui.Name), AvatarURL: strPtr(ui.Picture),
		RawProfile: mapFromJSON(ui.Raw),
	}
	if err := f.store.UserIdentities.Create(ctx, id); err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}
	_ = f.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &u.ID, ActorTenantID: &mem.TenantID,
		Action: "user.identity_linked",
		ResourceType: strPtr("user_identity"), ResourceID: &id.ID,
		IP: parseIP(in.IP), UserAgent: strPtr(in.UA),
		Metadata: map[string]any{"provider": string(provider)},
	})
	return f.signTokens(ctx, u.ID, mem.TenantID, mem.Role, in)
}

func (f *Flow) createNewUserAndIssue(ctx context.Context, provider store.IdentityProvider, ui UserInfo, in CallbackInput) (*CallbackResult, error) {
	userID := uuidMustV7()
	tenantID := uuidMustV7()
	memID := uuidMustV7()
	subID := uuidMustV7()

	freePlan, err := f.store.Plans.GetByCode(ctx, plan.CodeFree)
	if err != nil {
		return nil, fmt.Errorf("free plan lookup: %w", err)
	}

	displayName := ui.Name
	if displayName == "" {
		displayName = "User"
	}
	email := ui.Email
	if email == "" {
		email = ui.Sub + "@" + string(provider) + ".placeholder"
	}

	// We use a random unguessable password hash — the user can later set a real password.
	hash, err := auth.HashPassword(uuid.Must(uuid.NewV7()).String())
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}

	now := time.Now().UTC()
	u := &user.User{
		ID: userID, Email: email, PasswordHash: hash, DisplayName: displayName,
		Status:   user.StatusActive,
		Metadata: map[string]any{},
	}
	tn := &tenant.Tenant{
		ID: tenantID, Name: displayName + " workspace",
		Slug: "oauth-" + tenantID.String()[:8],
		Status: tenant.StatusActive, Metadata: map[string]any{},
	}
	mem := &membership.Membership{
		ID: memID, TenantID: tenantID, UserID: userID,
		Role: membership.RoleOwner, Status: membership.StatusActive,
	}
	sub := &subscription.Subscription{
		ID: subID, TenantID: tenantID, PlanID: freePlan.ID,
		Status: subscription.StatusActive,
		StartedAt: now, CurrentPeriodStart: now, CurrentPeriodEnd: now.Add(30 * 24 * time.Hour),
		Metadata: map[string]any{},
	}
	identity := &store.UserIdentity{
		ID: uuidMustV7(), UserID: userID, TenantID: tenantID,
		Provider: provider, ProviderSub: ui.Sub,
		Email: strPtr(ui.Email), EmailVerified: ui.EmailVerified,
		DisplayName: strPtr(ui.Name), AvatarURL: strPtr(ui.Picture),
		RawProfile: mapFromJSON(ui.Raw),
	}

	if err := f.store.WithTx(ctx, func(tx pgx_tx) error {
		if err := f.store.Users.WithTx(tx).Create(ctx, u); err != nil {
			return err
		}
		if err := f.store.Tenants.WithTx(tx).Create(ctx, tn); err != nil {
			return err
		}
		if err := f.store.Memberships.WithTx(tx).Create(ctx, mem); err != nil {
			return err
		}
		if err := f.store.Subscriptions.WithTx(tx).Create(ctx, sub); err != nil {
			return err
		}
		return f.store.UserIdentities.WithTx(tx).Create(ctx, identity)
	}); err != nil {
		return nil, fmt.Errorf("oauth create tx: %w", err)
	}

	_ = f.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &userID, ActorTenantID: &tenantID,
		Action: "user.registered",
		ResourceType: strPtr("user"), ResourceID: &userID,
		IP: parseIP(in.IP), UserAgent: strPtr(in.UA),
		Metadata: map[string]any{"via": "oauth:" + string(provider)},
	})

	return f.signTokens(ctx, userID, tenantID, mem.Role, in)
}

func (f *Flow) signTokens(ctx context.Context, userID, tenantID uuid.UUID, role membership.Role, in CallbackInput) (*CallbackResult, error) {
	sessionID, _ := uuid.NewV7()
	refresh, refreshHash, _ := auth.NewRefreshToken()
	ip, ua := in.IP, in.UA
	_ = sessionID
	rs := &store.RefreshSession{
		ID: sessionID, UserID: userID, TenantID: tenantID,
		TokenHash: refreshHash, Status: "active",
		UserAgent: strPtrOrNil(ua), IP: parseIP(ip),
		ExpiresAt: time.Now().UTC().Add(f.cfg.RefreshTokenTTL),
	}
	if err := f.store.Refresh.Create(ctx, rs); err != nil {
		return nil, fmt.Errorf("create refresh: %w", err)
	}
	accessTok, expiresAt, err := f.jwt.Sign(auth.SignInput{
		UserID: userID, TenantID: tenantID, SessionID: sessionID, Role: string(role),
	})
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	_ = f.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &userID, ActorTenantID: &tenantID,
		Action: "oauth.callback_completed",
		ResourceType: strPtr("identity"), ResourceID: &userID,
		IP: parseIP(in.IP), UserAgent: strPtr(in.UA),
		Metadata: map[string]any{"provider": string(role)}, // provider stored elsewhere; keep simple
	})

	return &CallbackResult{
		AccessToken:  accessTok,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(time.Until(expiresAt).Seconds()),
	}, nil
}

// ── helpers ──────────────────────────────────────────────────────────

func uuidMustV7() uuid.UUID {
	u, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return u
}

func strPtr(s string) *string { return &s }

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func parseIP(s string) *netip.Addr {
	if s == "" {
		return nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return &addr
}

func mapFromJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	_ = jsonUnmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// pgx_tx alias so we don't have to repeat pgx.Tx spelling in every call.
type pgx_tx = pgx.Tx

// jsonUnmarshal wraps encoding/json for the helper above.
func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}