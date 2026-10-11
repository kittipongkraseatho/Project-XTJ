package r2http

import (
	"encoding/json"
	"time"

	"github.com/trading-journal/api/internal/domain/behavior"
	"github.com/trading-journal/api/internal/domain/strategy"
)

type behaviorProfileRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type behaviorProfileResponse struct {
	ID               string                 `json:"id"`
	TradingAccountID string                 `json:"trading_account_id"`
	WindowStart      time.Time              `json:"window_start"`
	WindowEnd        time.Time              `json:"window_end"`
	AlgorithmVersion string                 `json:"algorithm_version"`
	Status           behavior.ProfileStatus `json:"status"`
	SampleSize       int                    `json:"sample_size"`
	Metrics          behavior.Metrics       `json:"metrics"`
	Findings         []findingResponse      `json:"findings"`
	CreatedAt        time.Time              `json:"created_at"`
}

type findingResponse struct {
	Code        behavior.FindingCode     `json:"code"`
	Severity    behavior.FindingSeverity `json:"severity"`
	Count       int                      `json:"count"`
	EvidenceIDs []string                 `json:"evidence_trade_ids"`
	EvidenceAt  []time.Time              `json:"evidence_timestamps"`
}

type strategyCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type strategyVersionRequest struct {
	Rules json.RawMessage `json:"rules"`
}

type strategyLinkRequest struct {
	StrategyVersionID string `json:"strategy_version_id"`
}

type strategyResponse struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Status           strategy.Status `json:"status"`
	CurrentVersionID *string         `json:"current_version_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type strategyVersionResponse struct {
	ID            string           `json:"id"`
	StrategyID    string           `json:"strategy_id"`
	VersionNo     int              `json:"version_no"`
	SchemaVersion int              `json:"schema_version"`
	Rules         strategy.RuleSet `json:"rules"`
	CreatedBy     string           `json:"created_by"`
	CreatedAt     time.Time        `json:"created_at"`
}

func profileDTO(profile *behavior.Profile) behaviorProfileResponse {
	out := behaviorProfileResponse{
		ID:               profile.ID.String(),
		TradingAccountID: profile.TradingAccountID.String(),
		WindowStart:      profile.WindowStart.UTC(),
		WindowEnd:        profile.WindowEnd.UTC(),
		AlgorithmVersion: profile.AlgorithmVersion,
		Status:           profile.Status,
		SampleSize:       profile.SampleSize,
		Metrics:          profile.Metrics,
		Findings:         make([]findingResponse, 0, len(profile.Findings)),
		CreatedAt:        profile.CreatedAt.UTC(),
	}
	for _, finding := range profile.Findings {
		ids := make([]string, 0, len(finding.EvidenceIDs))
		for _, id := range finding.EvidenceIDs {
			ids = append(ids, id.String())
		}
		out.Findings = append(out.Findings, findingResponse{
			Code: finding.Code, Severity: finding.Severity, Count: finding.Count,
			EvidenceIDs: ids, EvidenceAt: finding.EvidenceAt,
		})
	}
	return out
}

func strategyDTO(item *strategy.Strategy) strategyResponse {
	out := strategyResponse{
		ID: item.ID.String(), Name: item.Name, Description: item.Description,
		Status: item.Status, CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(),
	}
	if item.CurrentVersionID != nil {
		v := item.CurrentVersionID.String()
		out.CurrentVersionID = &v
	}
	return out
}

func versionDTO(item *strategy.Version) strategyVersionResponse {
	return strategyVersionResponse{
		ID: item.ID.String(), StrategyID: item.StrategyID.String(), VersionNo: item.VersionNo,
		SchemaVersion: item.SchemaVersion, Rules: item.Rules, CreatedBy: item.CreatedBy.String(),
		CreatedAt: item.CreatedAt.UTC(),
	}
}
