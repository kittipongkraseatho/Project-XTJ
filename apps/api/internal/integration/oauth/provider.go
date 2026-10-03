// Package oauth provides the OAuth 2.0 provider abstraction.
//
// Implementations live alongside this file (google.go, line.go, github.go — Phase 1).
// All implementations are STATELESS — they read credentials from config and use
// net/http for token + userinfo endpoints.
package oauth

import (
	"context"
	"encoding/json"

	"github.com/trading-journal/api/internal/store"
)

// TokenResponse holds the result of exchanging an authorization code.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	TokenType    string
	ExpiresIn    int // seconds
	Scope        string
}

// UserInfo is the normalized user profile from any provider.
type UserInfo struct {
	Sub           string          // stable provider user id (never changes for that user)
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Raw           json.RawMessage // full provider payload for raw_profile storage
}

// Provider is the OAuth 2.0 contract every provider must implement.
//
// All methods are safe for concurrent use. The interface is intentionally
// minimal — adding a new provider is purely a configuration + impl task.
type Provider interface {
	Name() store.IdentityProvider
	AuthorizeURL(state, codeChallenge string, redirectURI string) string
	ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI string) (TokenResponse, error)
	FetchUserInfo(ctx context.Context, accessToken string) (UserInfo, error)
}