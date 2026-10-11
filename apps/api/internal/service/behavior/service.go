package behavior

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

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

type atomicProfileStore interface {
	Save(context.Context, *domainbehavior.Profile, []domainbehavior.Finding) error
}

func NewService(trades TradeReader, profiles ProfileStore, findings FindingStore) *Service {
	return &Service{
		trades:   trades,
		profiles: profiles,
		findings: findings,
		calc:     NewCalculator(),
	}
}

// NewStoreService wires the production repositories with an atomic profile
// and finding persistence boundary.
func NewStoreService(st *store.Store) *Service {
	return NewService(st.Trades, storeProfileStore{store: st}, st.BehaviorFindings)
}

type storeProfileStore struct{ store *store.Store }

func (s storeProfileStore) Upsert(ctx context.Context, profile *domainbehavior.Profile) error {
	return s.store.BehaviorProfiles.Upsert(ctx, profile)
}

func (s storeProfileStore) GetLatest(ctx context.Context, tenantID, accountID uuid.UUID) (*domainbehavior.Profile, error) {
	return s.store.BehaviorProfiles.GetLatest(ctx, tenantID, accountID)
}

func (s storeProfileStore) Save(ctx context.Context, profile *domainbehavior.Profile, findings []domainbehavior.Finding) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.store.BehaviorProfiles.WithTx(tx).Upsert(ctx, profile); err != nil {
			return err
		}
		return s.store.BehaviorFindings.WithTx(tx).ReplaceForProfile(ctx, profile.TenantID, profile.ID, findings)
	})
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
	if saver, ok := s.profiles.(atomicProfileStore); ok {
		if err := saver.Save(ctx, &profile, profile.Findings); err != nil {
			return nil, err
		}
	} else {
		if err := s.profiles.Upsert(ctx, &profile); err != nil {
			return nil, err
		}
		if err := s.findings.ReplaceForProfile(ctx, tenantID, profile.ID, profile.Findings); err != nil {
			return nil, err
		}
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
