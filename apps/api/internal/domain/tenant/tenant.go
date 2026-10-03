package tenant

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

type Tenant struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Status    Status
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}