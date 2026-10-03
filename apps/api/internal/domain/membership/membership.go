package membership

import (
	"time"

	"github.com/gofrs/uuid"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusInvited   Status = "invited"
	StatusSuspended Status = "suspended"
)

type Membership struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	UserID    uuid.UUID
	Role      Role
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}