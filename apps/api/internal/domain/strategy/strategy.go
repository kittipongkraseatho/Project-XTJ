package strategy

import (
	"encoding/json"
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusDraft    Status = "draft"
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

type Strategy struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	OwnerUserID      uuid.UUID
	Name             string
	Description      string
	Status           Status
	CurrentVersionID *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Version struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	StrategyID    uuid.UUID
	VersionNo     int
	SchemaVersion int
	Rules         RuleSet
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
}

type RuleSet struct {
	SchemaVersion int         `json:"schema_version"`
	Setup         Setup       `json:"setup"`
	Entry         []Condition `json:"entry"`
	Exit          []Condition `json:"exit"`
	Risk          RiskIntent  `json:"risk"`
}

type Setup struct {
	Symbols    []string `json:"symbols"`
	Directions []string `json:"directions"`
	Sessions   []string `json:"sessions,omitempty"`
}

type Condition struct {
	Field    string          `json:"field"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value"`
}

type RiskIntent struct {
	StopDistance   float64 `json:"stop_distance"`
	TargetDistance float64 `json:"target_distance"`
	MaxVolume      float64 `json:"max_volume"`
}
