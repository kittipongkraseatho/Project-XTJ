package observability

import (
	"errors"
	"net/http"
	"testing"
)

func TestError_Format(t *testing.T) {
	e := New("CODE", "message", 400)
	if e.Error() != "CODE: message" {
		t.Errorf("got %q", e.Error())
	}
	if e.WithCause(errors.New("root")).Error() != "CODE: message: root" {
		t.Errorf("got %q", e.Error())
	}
}

func TestError_HTTPStatus(t *testing.T) {
	if HTTPStatus(nil) != 500 {
		t.Error("nil should be 500")
	}
	if HTTPStatus(ErrUserNotFound) != http.StatusNotFound {
		t.Errorf("ErrUserNotFound status = %d", HTTPStatus(ErrUserNotFound))
	}
	if HTTPStatus(ErrEmailTaken) != http.StatusConflict {
		t.Errorf("ErrEmailTaken status = %d", HTTPStatus(ErrEmailTaken))
	}
	if HTTPStatus(ErrRateLimited) != http.StatusTooManyRequests {
		t.Errorf("ErrRateLimited status = %d", HTTPStatus(ErrRateLimited))
	}
}

func TestFromError_EnvelopeShape(t *testing.T) {
	env := FromError(ErrEmailTaken)
	if env.Error.Code != "EMAIL_ALREADY_EXISTS" {
		t.Errorf("code=%s", env.Error.Code)
	}
	if env.Error.Message == "" {
		t.Error("message should be populated")
	}

	// Unknown error → INTERNAL_ERROR
	env2 := FromError(errors.New("mystery"))
	if env2.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("unknown should be INTERNAL_ERROR, got %s", env2.Error.Code)
	}
}

func TestAsDomain_ExtractsTyped(t *testing.T) {
	wrapped := ErrEmailTaken.WithCause(errors.New("db"))
	if d := AsDomain(wrapped); d == nil || d.Code != "EMAIL_ALREADY_EXISTS" {
		t.Error("AsDomain should extract the typed error")
	}
	if d := AsDomain(errors.New("plain")); d != nil {
		t.Error("AsDomain of plain should be nil")
	}
}

func TestErrorCatalog_Coverage(t *testing.T) {
	// Smoke-test that all spec §7.5 catalog entries are present.
	wantCodes := []string{
		"VALIDATION_FAILED", "UNAUTHORIZED", "INVALID_CREDENTIALS",
		"PASSWORD_TOO_WEAK", "EMAIL_ALREADY_EXISTS",
		"USER_NOT_FOUND", "TENANT_NOT_FOUND", "MEMBERSHIP_NOT_FOUND",
		"INSUFFICIENT_ROLE", "REFRESH_TOKEN_INVALID", "REFRESH_TOKEN_EXPIRED",
		"REFRESH_TOKEN_REUSE_DETECTED", "INTERNAL_ERROR", "RATE_LIMITED",
	}
	have := map[string]bool{}
	for _, c := range []*Error{
		ErrValidation, ErrUnauthorized, ErrInvalidCredentials,
		ErrPasswordTooWeak, ErrEmailTaken, ErrUserNotFound,
		ErrTenantNotFound, ErrMembershipNotFound, ErrInsufficientRole,
		ErrRefreshTokenInvalid, ErrRefreshTokenExpired, ErrRefreshTokenReuse,
		ErrInternal, ErrRateLimited,
	} {
		have[c.Code] = true
	}
	for _, w := range wantCodes {
		if !have[w] {
			t.Errorf("missing error code %q in catalog", w)
		}
	}
}