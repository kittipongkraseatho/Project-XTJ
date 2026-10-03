package imported_file

import (
	"time"

	"github.com/gofrs/uuid"
)

type Format string

const (
	FormatMT5HTML    Format = "mt5_html"
	FormatMT5CSV     Format = "mt5_csv"
	FormatGenericCSV Format = "generic_csv"
)

type ImportedFile struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	TradingAccountID  uuid.UUID
	UserID            uuid.UUID
	Format            Format
	OriginalFilename  string
	ByteSize          int64
	ContentHash       string
	StoragePath       *string
	Metadata          map[string]any
	CreatedAt         time.Time
}