package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/trading-journal/api/internal/store"
)

// LineProvider implements OAuth 2.0 Authorization Code + PKCE for LINE.
//
// LINE-specific quirks:
//   - authorize: https://access.line.me/oauth2/v2.1/authorize
//   - token:     https://api.line.me/oauth2/v2.1/token
//   - profile:   https://api.line.me/v2/profile (limited fields)
//   - email is OPTIONAL — must request scope "openid email" AND user must grant it
//   - "sub" maps to userId
//   - PKCE IS supported (spec v2.1+)
type LineProvider struct {
	clientID     string
	clientSecret string
	redirectURI  string
	http         *http.Client
}

func NewLineProvider(clientID, clientSecret, redirectURI string) *LineProvider {
	return &LineProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (l *LineProvider) Name() store.IdentityProvider { return store.ProviderLine }

func (l *LineProvider) AuthorizeURL(state, codeChallenge string, redirectURI string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", l.clientID)
	v.Set("redirect_uri", coalesce(redirectURI, l.redirectURI))
	v.Set("state", state)
	v.Set("scope", "openid email profile")
	v.Set("code_challenge", codeChallenge)
	v.Set("code_challenge_method", "S256")
	return "https://access.line.me/oauth2/v2.1/authorize?" + v.Encode()
}

func (l *LineProvider) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI string) (TokenResponse, error) {
	v := url.Values{}
	v.Set("grant_type", "authorization_code")
	v.Set("code", code)
	v.Set("redirect_uri", coalesce(redirectURI, l.redirectURI))
	v.Set("client_id", l.clientID)
	v.Set("client_secret", l.clientSecret)
	v.Set("code_verifier", codeVerifier)
	return doTokenExchange(ctx, l.http, "https://api.line.me/oauth2/v2.1/token", v)
}

func (l *LineProvider) FetchUserInfo(ctx context.Context, accessToken string) (UserInfo, error) {
	// LINE returns basic profile via /v2/profile
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.line.me/v2/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := l.http.Do(req)
	if err != nil {
		return UserInfo{}, fmt.Errorf("line profile: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return UserInfo{}, fmt.Errorf("line profile status %d: %s", resp.StatusCode, string(b))
	}
	raw, _ := io.ReadAll(resp.Body)
	var p struct {
		UserID        string `json:"userId"`
		DisplayName    string `json:"displayName"`
		PictureURL     string `json:"pictureUrl"`
		StatusMessage  string `json:"statusMessage"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return UserInfo{}, fmt.Errorf("line profile decode: %w", err)
	}

	// LINE email (if scope granted) is in the ID token's payload, not the profile endpoint.
	// Decode ID token for email. Without an ID token, email_verified is false.
	// For Phase 0 we keep this simple — email remains empty if not provided.
	return UserInfo{
		Sub:           p.UserID,
		Email:         "",
		EmailVerified: false,
		Name:          p.DisplayName,
		Picture:       p.PictureURL,
		Raw:           raw,
	}, nil
}