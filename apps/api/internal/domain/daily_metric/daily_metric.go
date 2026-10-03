package daily_metric

import (
	"time"

	"github.com/gofrs/uuid"
)

type DailyMetric struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	TradingAccountID *uuid.UUID
	TradeDate        time.Time
	TradeCount       int
	WinCount         int
	LossCount        int
	GrossProfit      float64
	GrossLoss        float64
	NetPL            float64
	Commission       float64
	Swap             float64
	EndingEquity     *float64
	Metadata         map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
}