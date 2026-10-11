package behavior

import (
	"time"

	"github.com/gofrs/uuid"
)

type ProfileStatus string

const (
	ProfileStatusInsufficientData ProfileStatus = "insufficient_data"
	ProfileStatusReady            ProfileStatus = "ready"
)

type FindingCode string

const (
	FindingOvertradingFrequency FindingCode = "overtrading_frequency"
	FindingRevengeAfterLoss     FindingCode = "revenge_after_loss"
	FindingLossStreak           FindingCode = "loss_streak"
	FindingRiskEscalation       FindingCode = "risk_escalation"
	FindingSymbolConcentration  FindingCode = "symbol_concentration"
)

type FindingSeverity string

const (
	SeverityInfo     FindingSeverity = "info"
	SeverityWarning  FindingSeverity = "warning"
	SeverityCritical FindingSeverity = "critical"
)

type InputTrade struct {
	ID        uuid.UUID
	Symbol    string
	Direction string
	Volume    float64
	NetPL     float64
	Outcome   string
	OpenTime  time.Time
	CloseTime *time.Time
	Magic     *int64
}

type Metrics struct {
	WinRate         float64            `json:"win_rate"`
	Expectancy      float64            `json:"expectancy"`
	AverageWin      float64            `json:"average_win"`
	AverageLoss     float64            `json:"average_loss"`
	ProfitFactor    float64            `json:"profit_factor"`
	NetPL           float64            `json:"net_pl"`
	AverageHoldSecs float64            `json:"average_hold_seconds"`
	TradesPerDay    float64            `json:"trades_per_day"`
	SymbolShare     map[string]float64 `json:"symbol_share"`
	DirectionShare  map[string]float64 `json:"direction_share"`
	SessionShare    map[string]float64 `json:"session_share"`
	MagicShare      map[string]float64 `json:"magic_share"`
}

type Profile struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	TradingAccountID uuid.UUID
	WindowStart      time.Time
	WindowEnd        time.Time
	AlgorithmVersion string
	Status           ProfileStatus
	SampleSize       int
	Metrics          Metrics
	Findings         []Finding
	CreatedAt        time.Time
}

type Finding struct {
	Code        FindingCode
	Severity    FindingSeverity
	Count       int
	EvidenceIDs []uuid.UUID
	EvidenceAt  []time.Time
}
