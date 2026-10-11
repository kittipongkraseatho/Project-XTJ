package strategy

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	domainstrategy "github.com/trading-journal/api/internal/domain/strategy"
)

type strategyRepoStub struct {
	created *domainstrategy.Strategy
}

func (s *strategyRepoStub) Create(_ context.Context, item *domainstrategy.Strategy) error {
	s.created = item
	return nil
}

func (s *strategyRepoStub) ListByTenant(context.Context, uuid.UUID) ([]*domainstrategy.Strategy, error) {
	return nil, nil
}

func (s *strategyRepoStub) GetByIDByTenant(context.Context, uuid.UUID, uuid.UUID) (*domainstrategy.Strategy, error) {
	return nil, nil
}

type versionCreatorStub struct {
	called bool
}

func (s *versionCreatorStub) CreateNext(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, _ domainstrategy.RuleSet) (*domainstrategy.Version, error) {
	s.called = true
	return &domainstrategy.Version{}, nil
}

type versionReaderStub struct{}

func (versionReaderStub) GetByIDByTenant(context.Context, uuid.UUID, uuid.UUID) (*domainstrategy.Version, error) {
	return &domainstrategy.Version{}, nil
}

func (versionReaderStub) ListByStrategy(context.Context, uuid.UUID, uuid.UUID) ([]*domainstrategy.Version, error) {
	return nil, nil
}

type tradeLinkerStub struct {
	called bool
}

func (s *tradeLinkerStub) LinkStrategyVersion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	s.called = true
	return nil
}

func validRules() domainstrategy.RuleSet {
	return domainstrategy.RuleSet{
		SchemaVersion: 1,
		Setup:         domainstrategy.Setup{Symbols: []string{"EURUSD"}, Directions: []string{"long"}},
		Entry:         []domainstrategy.Condition{{Field: "rsi", Operator: "lt", Value: []byte("30")}},
		Exit:          []domainstrategy.Condition{{Field: "rsi", Operator: "gt", Value: []byte("70")}},
		Risk:          domainstrategy.RiskIntent{StopDistance: 10, TargetDistance: 20, MaxVolume: 1},
	}
}

func TestServiceCreateDefaultsDraftAndGeneratesID(t *testing.T) {
	repo := &strategyRepoStub{}
	svc := NewService(repo, &versionCreatorStub{}, versionReaderStub{}, &tradeLinkerStub{})

	item, err := svc.Create(context.Background(), CreateInput{
		TenantID:    uuid.Must(uuid.NewV7()),
		OwnerUserID: uuid.Must(uuid.NewV7()),
		Name:        "Breakout",
	})

	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, item.ID)
	require.Equal(t, domainstrategy.StatusDraft, item.Status)
	require.Same(t, item, repo.created)
}

func TestServiceCreateVersionRejectsInvalidRulesBeforePersistence(t *testing.T) {
	versions := &versionCreatorStub{}
	svc := NewService(&strategyRepoStub{}, versions, versionReaderStub{}, &tradeLinkerStub{})

	_, err := svc.CreateVersion(context.Background(), uuid.Must(uuid.NewV7()), CreateVersionInput{
		StrategyID: uuid.Must(uuid.NewV7()),
		CreatedBy:  uuid.Must(uuid.NewV7()),
		Rules:      domainstrategy.RuleSet{SchemaVersion: 1},
	})

	require.ErrorIs(t, err, domainstrategy.ErrInvalidRuleSet)
	require.False(t, versions.called)
}
