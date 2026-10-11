package behavior

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
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
}

type Service struct {
	trades   TradeReader
	profiles ProfileStore
	findings FindingStore
	calc     *Calculator
}

func NewService(trades TradeReader, profiles ProfileStore, findings FindingStore) *Service {
	return &Service{
		trades:   trades,
		profiles: profiles,
		findings: findings,
		calc:     NewCalculator(),
	}
}

func (s *Service) Analyze(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time) (*domainbehavior.Profile, error) {
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
	return &profile, nil
}

func (s *Service) Latest(ctx context.Context, tenantID, accountID uuid.UUID) (*domainbehavior.Profile, error) {
	return s.profiles.GetLatest(ctx, tenantID, accountID)
}
