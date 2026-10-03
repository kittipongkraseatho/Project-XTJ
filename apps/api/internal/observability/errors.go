// Package observability errors — typed domain errors + uniform JSON envelope.
//
// All service-level errors flow through *Error. The envelope is the ONLY shape
// clients see; never leak Go error text.
package observability

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is a typed domain error with a stable machine-readable Code,
// human-readable Message, optional Details map, and an optional wrapped Cause
// (for logging only — not returned to clients).
type Error struct {
	Code    string         // machine-readable, stable across versions
	Message string         // human text (English, i18n in Phase 4)
	HTTP    int            // mapped HTTP status
	Details map[string]any // field-level validation etc.
	Cause   error          // wrapped root cause (logged, never returned)
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// New constructs a new *Error.
func New(code, message string, http int) *Error {
	return &Error{Code: code, Message: message, HTTP: http}
}

// WithCause wraps the underlying error (for logging).
func (e *Error) WithCause(cause error) *Error {
	e.Cause = cause
	return e
}

// WithDetails attaches a field-level details map.
func (e *Error) WithDetails(d map[string]any) *Error {
	e.Details = d
	return e
}

// AsDomain extracts an *Error from any wrapped chain, or nil.
func AsDomain(err error) *Error {
	var de *Error
	if errors.As(err, &de) {
		return de
	}
	return nil
}

// ── Stable error catalog (spec §7.5) ──────────────────────────────────

var (
	ErrValidation = &Error{Code: "VALIDATION_FAILED", Message: "request validation failed", HTTP: http.StatusUnprocessableEntity}
	ErrUnauthorized = &Error{Code: "UNAUTHORIZED", Message: "missing or invalid credentials", HTTP: http.StatusUnauthorized}
	ErrInvalidCredentials = &Error{Code: "INVALID_CREDENTIALS", Message: "invalid email or password", HTTP: http.StatusUnauthorized}
	ErrPasswordTooWeak = &Error{Code: "PASSWORD_TOO_WEAK", Message: "password does not meet policy", HTTP: http.StatusUnprocessableEntity}
	ErrEmailTaken = &Error{Code: "EMAIL_ALREADY_EXISTS", Message: "email is already registered", HTTP: http.StatusConflict}
	ErrUserNotFound = &Error{Code: "USER_NOT_FOUND", Message: "user not found", HTTP: http.StatusNotFound}
	ErrTenantNotFound = &Error{Code: "TENANT_NOT_FOUND", Message: "tenant not found", HTTP: http.StatusNotFound}
	ErrMembershipNotFound = &Error{Code: "MEMBERSHIP_NOT_FOUND", Message: "membership not found", HTTP: http.StatusForbidden}
	ErrInsufficientRole = &Error{Code: "INSUFFICIENT_ROLE", Message: "role does not permit this action", HTTP: http.StatusForbidden}
	ErrRefreshTokenInvalid = &Error{Code: "REFRESH_TOKEN_INVALID", Message: "refresh token is invalid", HTTP: http.StatusUnauthorized}
	ErrRefreshTokenExpired = &Error{Code: "REFRESH_TOKEN_EXPIRED", Message: "refresh token has expired", HTTP: http.StatusUnauthorized}
	ErrRefreshTokenReuse = &Error{Code: "REFRESH_TOKEN_REUSE_DETECTED", Message: "refresh token reuse detected; all sessions revoked", HTTP: http.StatusUnauthorized}
	ErrRateLimited = &Error{Code: "RATE_LIMITED", Message: "too many requests", HTTP: http.StatusTooManyRequests}
	ErrTenantMissingFromContext = &Error{Code: "INTERNAL_ERROR", Message: "tenant context not propagated", HTTP: http.StatusInternalServerError}
	ErrInternal = &Error{Code: "INTERNAL_ERROR", Message: "internal server error", HTTP: http.StatusInternalServerError}
)

// OAuth-specific
var (
	ErrOAuthStateInvalid = &Error{Code: "OAUTH_STATE_INVALID", Message: "oauth state is invalid or expired", HTTP: http.StatusBadRequest}
	ErrOAuthProviderNotConfigured = &Error{Code: "OAUTH_PROVIDER_NOT_CONFIGURED", Message: "oauth provider is not configured", HTTP: http.StatusBadRequest}
	ErrOAuthCodeExchange = &Error{Code: "OAUTH_CODE_EXCHANGE_FAILED", Message: "oauth code exchange failed", HTTP: http.StatusBadGateway}
	ErrOAuthUserInfo = &Error{Code: "OAUTH_USERINFO_FAILED", Message: "oauth userinfo fetch failed", HTTP: http.StatusBadGateway}
	ErrParseFailed = &Error{Code: "PARSE_FAILED", Message: "failed to parse file", HTTP: http.StatusUnprocessableEntity}
	ErrImportFailed = &Error{Code: "IMPORT_FAILED", Message: "import failed", HTTP: http.StatusUnprocessableEntity}
)

// ── JSON envelope ────────────────────────────────────────────────────

// Envelope is the wire format returned to clients on any error path.
type Envelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// FromError converts any error into the envelope shape. Unknown errors collapse
// to a generic INTERNAL_ERROR (logged in full at the middleware layer).
func FromError(err error) Envelope {
	if de := AsDomain(err); de != nil {
		return Envelope{Error: ErrorBody{Code: de.Code, Message: de.Message, Details: de.Details}}
	}
	return Envelope{Error: ErrorBody{Code: ErrInternal.Code, Message: ErrInternal.Message}}
}

// HTTPStatus returns the status for an error, defaulting to 500.
func HTTPStatus(err error) int {
	if de := AsDomain(err); de != nil {
		return de.HTTP
	}
	return http.StatusInternalServerError
}