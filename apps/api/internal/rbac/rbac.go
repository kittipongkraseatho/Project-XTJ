// Package rbac provides role-based access control middleware and helpers.
//
// Phase 0 matrix per spec §7.4 — every Phase 0 user is the sole 'owner' of
// their tenant, but the matrix is forward-compatible.
package rbac

import (
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/observability"
)

// Role string constants (must match DB enum `membership_role`).
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// RoleRank returns the privilege rank for a role string.
// Unknown roles rank 0 (below viewer).
func RoleRank(role string) int {
	switch role {
	case RoleOwner:
		return 40
	case RoleAdmin:
		return 30
	case RoleMember:
		return 20
	case RoleViewer:
		return 10
	}
	return 0
}

// HasRole returns true iff the caller's role rank >= minRole rank.
func HasRole(actual, minRole string) bool {
	return RoleRank(actual) >= RoleRank(minRole)
}

// RequireRole returns a middleware that ensures the JWT role is at least
// the given minRole. Mount AFTER AuthMiddleware.
//
// On insufficient role returns 403 INSUFFICIENT_ROLE envelope.
func RequireRole(minRole string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role, _ := c.Get("role").(string)
			if !HasRole(role, minRole) {
				return c.JSON(403, observability.FromError(
					observability.ErrInsufficientRole.WithDetails(map[string]any{
						"required": minRole,
						"actual":   role,
					}),
				))
			}
			return next(c)
		}
	}
}