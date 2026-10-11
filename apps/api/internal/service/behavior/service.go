package behavior

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
	"github.com/trading-journal/api/internal/store"
)

var ErrInvalidWindow = errors.New("behavior: window must end after it starts")

type TradeReader interface {
	ListCompletedByAccountWindow(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]domainbehavior.InputTrade, error)
}

type ProfileStore interface {
	Upsert(context.Context, *domainbehavior.Profile) error
	GetLatest(context.Context, uuid.UUID, uuid.UUID) (*domainbehavior.Profile, error)
}

type FindingStore interface {
	ReplaceForProfile(context.Context, uuid.UUID, uuid.UUID, []domainbehavior.Finding) error
	ListByProfile(context.Context, uuid.UUID, uuid.UUID) ([]domainbehavior.Finding, error)
}

type Auditor interface {
	Insert(context.Context, store.AuditEntry) error
}

type Service struct {
	trades   TradeReader
	profiles ProfileStore
	findings FindingStore
	calc     *Calculator
	audit    Auditor
}

func NewService(trades TradeReader, profiles ProfileStore, findings FindingStore) *Service {
	return &Service{
		trades:   trades,
		profiles: profiles,
		findings: findings,
		calc:     NewCalculator(),
	}
}

func (s *Service) SetAuditor(auditor Auditor) {
	s.audit = auditor
}

func (s *Service) Analyze(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time) (*domainbehavior.Profile, error) {
	return s.AnalyzeAs(ctx, tenantID, nil, accountID, from, to)
}

func (s *Service) AnalyzeAs(ctx context.Context, tenantID uuid.UUID, actorUserID *uuid.UUID, accountID uuid.UUID, from, to time.Time) (*domainbehavior.Profile, error) {
	from = from.UTC()
	to = to.UTC()
	if !to.After(from) {
		return nil, ErrInvalidWindow
	}
	inputs, err := s.trades.ListCompletedByAccountWindow(ctx, tenantID, accountID, from, to)
	if err != nil {
		return nil, err
	}
	profile := s.calc.Calculate(accountID, from, to, inputs)
	profile.TenantID = tenantID
	if profile.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		profile.ID = id
	}
	if err := s.profiles.Upsert(ctx, &profile); err != nil {
		return nil, err
	}
	if err := s.findings.ReplaceForProfile(ctx, tenantID, profile.ID, profile.Findings); err != nil {
		return nil, err
	}
	if s.audit != nil {
		resourceType := "trade_behavior_profile"
		_ = s.audit.Insert(ctx, store.AuditEntry{
			ActorUserID: actorUserID, ActorTenantID: &tenantID,
			Action: "behavior_profile_calculated", ResourceType: &resourceType, ResourceID: &profile.ID,
			Metadata: map[string]any{
				"account_id": profile.TradingAccountID.String(), "window_start": profile.WindowStart,
				"window_end": profile.WindowEnd, "sample_size": profile.SampleSize,
				"algorithm_version": profile.AlgorithmVersion,
			},
		})
	}
	return &profile, nil
}

func (s *Service) Latest(ctx context.Context, tenantID, accountID uuid.UUID) (*domainbehavior.Profile, error) {
	profile, err := s.profiles.GetLatest(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	profile.Findings, err = s.findings.ListByProfile(ctx, tenantID, profile.ID)
	if err != nil {
		return nil, err
	}
	return profile, nil
}
