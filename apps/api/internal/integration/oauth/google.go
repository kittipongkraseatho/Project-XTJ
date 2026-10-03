package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trading-journal/api/internal/store"
)

// GoogleProvider implements OAuth 2.0 Authorization Code + PKCE for Google.
//
// Endpoints (Phase 0: just openid+email+profile):
//   - authorize: https://accounts.google.com/o/oauth2/v2/auth
//   - token:     https://oauth2.googleapis.com/token
//   - userinfo:  https://openidconnect.googleapis.com/v1/userinfo
type GoogleProvider struct {
	clientID     string
	clientSecret string
	redirectURI  string
	http         *http.Client
}

func NewGoogleProvider(clientID, clientSecret, redirectURI string) *GoogleProvider {
	return &GoogleProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GoogleProvider) Name() store.IdentityProvider { return store.ProviderGoogle }

func (g *GoogleProvider) AuthorizeURL(state, codeChallenge string, redirectURI string) string {
	v := url.Values{}
	v.Set("client_id", g.clientID)
	v.Set("redirect_uri", coalesce(redirectURI, g.redirectURI))
	v.Set("response_type", "code")
	v.Set("scope", "openid email profile")
	v.Set("state", state)
	v.Set("code_challenge", codeChallenge)
	v.Set("code_challenge_method", "S256")
	v.Set("access_type", "online")
	v.Set("prompt", "select_account")
	return "https://accounts.google.com/o/oauth2/v2/auth?" + v.Encode()
}

func (g *GoogleProvider) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI string) (TokenResponse, error) {
	v := url.Values{}
	v.Set("code", code)
	v.Set("client_id", g.clientID)
	v.Set("client_secret", g.clientSecret)
	v.Set("redirect_uri", coalesce(redirectURI, g.redirectURI))
	v.Set("grant_type", "authorization_code")
	v.Set("code_verifier", codeVerifier)
	return doTokenExchange(ctx, g.http, "https://oauth2.googleapis.com/token", v)
}

func (g *GoogleProvider) FetchUserInfo(ctx context.Context, accessToken string) (UserInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://openidconnect.googleapis.com/v1/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := g.http.Do(req)
	if err != nil {
		return UserInfo{}, fmt.Errorf("google userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return UserInfo{}, fmt.Errorf("google userinfo status %d: %s", resp.StatusCode, string(b))
	}
	raw, _ := io.ReadAll(resp.Body)
	var p struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return UserInfo{}, fmt.Errorf("google userinfo decode: %w", err)
	}
	return UserInfo{
		Sub: p.Sub, Email: p.Email, EmailVerified: p.EmailVerified,
		Name: p.Name, Picture: p.Picture, Raw: raw,
	}, nil
}

// ── shared helpers ──────────────────────────────────────────────────

func doTokenExchange(ctx context.Context, h *http.Client, urlStr string, v url.Values) (TokenResponse, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", urlStr, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := h.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return TokenResponse{}, fmt.Errorf("token exchange status %d: %s", resp.StatusCode, string(b))
	}
	raw, _ := io.ReadAll(resp.Body)
	var t struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return TokenResponse{}, fmt.Errorf("token decode: %w", err)
	}
	return TokenResponse{
		AccessToken: t.AccessToken, RefreshToken: t.RefreshToken,
		IDToken: t.IDToken, TokenType: t.TokenType,
		ExpiresIn: t.ExpiresIn, Scope: t.Scope,
	}, nil
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}