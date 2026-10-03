package authhttp

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/integration/oauth"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// OAuthHandlers hosts the /v1/auth/oauth/* routes.
type OAuthHandlers struct {
	flow *oauth.Flow
	log  *slog.Logger
}

func NewOAuthHandlers(flow *oauth.Flow, log *slog.Logger) *OAuthHandlers {
	return &OAuthHandlers{flow: flow, log: log}
}

// Start redirects the user to the provider's authorize URL.
//
// GET /v1/auth/oauth/:provider/start
func (h *OAuthHandlers) Start(c echo.Context) error {
	provider, err := parseProvider(c.Param("provider"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(err))
	}
	res, err := h.flow.Start(c.Request().Context(), provider,
		c.RealIP(), c.Request().Header.Get("User-Agent"))
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.Redirect(http.StatusFound, res.AuthorizeURL)
}

// Callback handles the redirect-back from the OAuth provider.
//
// GET /v1/auth/oauth/:provider/callback?code=...&state=...
//
// On success: 302 to /auth/oauth-complete?access=...&refresh=...&provider=...
// On failure: 302 to /login?error=CODE&provider=...
func (h *OAuthHandlers) Callback(c echo.Context) error {
	provider, err := parseProvider(c.Param("provider"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(err))
	}
	res, err := h.flow.HandleCallback(c.Request().Context(), oauth.CallbackInput{
		Provider: provider,
		Code:     c.QueryParam("code"),
		State:    c.QueryParam("state"),
		IP:       c.RealIP(),
		UA:       c.Request().Header.Get("User-Agent"),
	})
	if err != nil {
		// Redirect to FE login with error code
		return c.Redirect(http.StatusFound, "/login?error="+string(errCode(err))+"&provider="+string(provider))
	}
	redirectTo := "/auth/oauth-complete?access=" + urlQueryEscape(res.AccessToken) +
		"&refresh=" + urlQueryEscape(res.RefreshToken) +
		"&expires_in=" + i64ToStr(res.ExpiresIn)
	return c.Redirect(http.StatusFound, redirectTo)
}

func parseProvider(s string) (store.IdentityProvider, error) {
	switch s {
	case "google":
		return store.ProviderGoogle, nil
	case "line":
		return store.ProviderLine, nil
	case "github":
		return store.ProviderGitHub, nil
	}
	return "", observability.ErrOAuthStateInvalid
}

func errCode(err error) string {
	if de := observability.AsDomain(err); de != nil {
		return de.Code
	}
	return "INTERNAL_ERROR"
}