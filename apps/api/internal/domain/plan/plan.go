package plan

import (
	"time"

	"github.com/gofrs/uuid"
)

type Code string

const (
	CodeFree           Code = "free"
	CodeUploadJournal  Code = "upload_journal"
	CodeStarter        Code = "starter"
	CodePro            Code = "pro"
)

type Interval string

const (
	IntervalMonthly Interval = "monthly"
	IntervalYearly  Interval = "yearly"
)

type Plan struct {
	ID          uuid.UUID
	Code        Code
	Name        string
	Description *string
	PriceCents  int64
	Currency    string
	Interval    Interval
	IsActive    bool
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}