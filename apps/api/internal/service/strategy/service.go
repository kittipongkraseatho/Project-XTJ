package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	domainstrategy "github.com/trading-journal/api/internal/domain/strategy"
	"github.com/trading-journal/api/internal/store"
)

var ErrNameRequired = errors.New("strategy: name is required")

type StrategyRepository interface {
	Create(context.Context, *domainstrategy.Strategy) error
	ListByTenant(context.Context, uuid.UUID) ([]*domainstrategy.Strategy, error)
	GetByIDByTenant(context.Context, uuid.UUID, uuid.UUID) (*domainstrategy.Strategy, error)
}

type VersionCreator interface {
	CreateNext(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domainstrategy.RuleSet) (*domainstrategy.Version, error)
}

type VersionReader interface {
	GetByIDByTenant(context.Context, uuid.UUID, uuid.UUID) (*domainstrategy.Version, error)
	ListByStrategy(context.Context, uuid.UUID, uuid.UUID) ([]*domainstrategy.Version, error)
}

type TradeLinker interface {
	LinkStrategyVersion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}

type Auditor interface {
	Insert(context.Context, store.AuditEntry) error
}

type Service struct {
	strategies StrategyRepository
	versions   VersionCreator
	versionGet VersionReader
	trades     TradeLinker
	audit      Auditor
}

func NewService(strategies StrategyRepository, versions VersionCreator, versionGet VersionReader, trades TradeLinker) *Service {
	return &Service{strategies: strategies, versions: versions, versionGet: versionGet, trades: trades}
}

func NewStoreService(st *store.Store) *Service {
	svc := NewService(st.Strategies, storeVersionCreator{store: st}, st.StrategyVersions, st.Trades)
	svc.audit = st.Audit
	return svc
}

type storeVersionCreator struct{ store *store.Store }

func (c storeVersionCreator) CreateNext(ctx context.Context, tenantID, strategyID, createdBy uuid.UUID, rules domainstrategy.RuleSet) (*domainstrategy.Version, error) {
	var version *domainstrategy.Version
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		version, err = c.store.StrategyVersions.WithTx(tx).CreateNext(ctx, tenantID, strategyID, createdBy, rules)
		return err
	})
	return version, err
}

type CreateInput struct {
	TenantID    uuid.UUID
	OwnerUserID uuid.UUID
	Name        string
	Description string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domainstrategy.Strategy, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrNameRequired
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	item := &domainstrategy.Strategy{
		ID:          id,
		TenantID:    in.TenantID,
		OwnerUserID: in.OwnerUserID,
		Name:        strings.TrimSpace(in.Name),
		Description: in.Description,
		Status:      domainstrategy.StatusDraft,
	}
	if err := s.strategies.Create(ctx, item); err != nil {
		return nil, err
	}
	if s.audit != nil {
		resourceType := "strategy"
		_ = s.audit.Insert(ctx, store.AuditEntry{
			ActorUserID: &item.OwnerUserID, ActorTenantID: &item.TenantID,
			Action: "strategy_created", ResourceType: &resourceType, ResourceID: &item.ID,
			Metadata: map[string]any{"name": item.Name},
		})
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, tenantID uuid.UUID) ([]*domainstrategy.Strategy, error) {
	return s.strategies.ListByTenant(ctx, tenantID)
}

func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (*domainstrategy.Strategy, error) {
	return s.strategies.GetByIDByTenant(ctx, tenantID, id)
}

type CreateVersionInput struct {
	StrategyID uuid.UUID
	CreatedBy  uuid.UUID
	Rules      domainstrategy.RuleSet
}

func (s *Service) CreateVersion(ctx context.Context, tenantID uuid.UUID, in CreateVersionInput) (*domainstrategy.Version, error) {
	raw, err := json.Marshal(in.Rules)
	if err != nil {
		return nil, err
	}
	if _, err := domainstrategy.ValidateRuleSet(raw); err != nil {
		return nil, err
	}
	version, err := s.versions.CreateNext(ctx, tenantID, in.StrategyID, in.CreatedBy, in.Rules)
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		resourceType := "strategy_version"
		_ = s.audit.Insert(ctx, store.AuditEntry{
			ActorUserID: &in.CreatedBy, ActorTenantID: &tenantID,
			Action: "strategy_version_created", ResourceType: &resourceType, ResourceID: &version.ID,
			Metadata: map[string]any{"strategy_id": version.StrategyID.String(), "version_no": version.VersionNo, "schema_version": version.SchemaVersion},
		})
	}
	return version, nil
}

func (s *Service) ListVersions(ctx context.Context, tenantID, strategyID uuid.UUID) ([]*domainstrategy.Version, error) {
	if _, err := s.strategies.GetByIDByTenant(ctx, tenantID, strategyID); err != nil {
		return nil, err
	}
	return s.versionGet.ListByStrategy(ctx, tenantID, strategyID)
}

func (s *Service) LinkTrade(ctx context.Context, tenantID, tradeID, versionID uuid.UUID) error {
	return s.LinkTradeAs(ctx, tenantID, nil, tradeID, versionID)
}

func (s *Service) LinkTradeAs(ctx context.Context, tenantID uuid.UUID, actorUserID *uuid.UUID, tradeID, versionID uuid.UUID) error {
	if _, err := s.versionGet.GetByIDByTenant(ctx, tenantID, versionID); err != nil {
		return err
	}
	if err := s.trades.LinkStrategyVersion(ctx, tenantID, tradeID, versionID); err != nil {
		return err
	}
	if s.audit != nil {
		resourceType := "trade"
		_ = s.audit.Insert(ctx, store.AuditEntry{
			ActorUserID: actorUserID, ActorTenantID: &tenantID,
			Action: "trade_strategy_version_linked", ResourceType: &resourceType, ResourceID: &tradeID,
			Metadata: map[string]any{"strategy_version_id": versionID.String()},
		})
	}
	return nil
}
