package import_job

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type ImportJob struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ImportedFileID uuid.UUID
	Status         Status
	StartedAt      *time.Time
	FinishedAt     *time.Time
	ParsedRows     int
	InsertedRows   int
	SkippedRows    int
	Errors         []map[string]any
	WorkerID       *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}