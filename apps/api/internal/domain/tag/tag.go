package tag

import (
	"time"

	"github.com/gofrs/uuid"
)

type Owner string

const (
	OwnerSystem Owner = "system"
	OwnerUser   Owner = "user"
)

type Tag struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	Color     *string
	Owner     Owner
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}