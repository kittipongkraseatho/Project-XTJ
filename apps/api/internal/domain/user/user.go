// Package user defines the User domain entity + repository contract.
package user

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusActive               Status = "active"
	StatusSuspended            Status = "suspended"
	StatusPendingVerification  Status = "pending_verification"
)

// User is the canonical user entity. Maps directly to the `users` table.
type User struct {
	ID                       uuid.UUID
	Email                    string
	PasswordHash             string
	DisplayName              string
	AvatarCloudinaryPublicID *string
	Status                   Status
	Metadata                 map[string]any
	CreatedAt                time.Time
	UpdatedAt                time.Time
	DeletedAt                *time.Time
}