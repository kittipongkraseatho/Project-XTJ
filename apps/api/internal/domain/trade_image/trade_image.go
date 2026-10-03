package trade_image

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusUploading Status = "uploading"
	StatusReady     Status = "ready"
	StatusFailed    Status = "failed"
	StatusDeleted   Status = "deleted"
)

type TradeImage struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
	TradeID               uuid.UUID
	UserID                uuid.UUID
	CloudinaryPublicID    string
	CloudinaryAssetID     *string
	ResourceType          string
	Format                string
	Width                 *int
	Height                *int
	Bytes                 int64
	SecureURL             string
	Folder                *string
	Version               *int64
	Status                Status
	DeletedAt             *time.Time
	CreatedAt             time.Time
}