package behavior

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
)

type behaviorTradeReaderStub struct {
	trades []domainbehavior.InputTrade
}

func (s behaviorTradeReaderStub) ListCompletedByAccountWindow(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]domainbehavior.InputTrade, error) {
	return s.trades, nil
}

type behaviorProfileWriterStub struct {
	profile  *domainbehavior.Profile
	findings []domainbehavior.Finding
}

func (s *behaviorProfileWriterStub) Upsert(_ context.Context, profile *domainbehavior.Profile) error {
	s.profile = profile
	return nil
}

func (s *behaviorProfileWriterStub) ReplaceForProfile(_ context.Context, _ uuid.UUID, _ uuid.UUID, findings []domainbehavior.Finding) error {
	s.findings = findings
	return nil
}

func (s *behaviorProfileWriterStub) GetLatest(context.Context, uuid.UUID, uuid.UUID) (*domainbehavior.Profile, error) {
	return s.profile, nil
}

func TestServiceAnalyzePersistsProfileAndFindings(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	trades := make([]domainbehavior.InputTrade, 10)
	for i := range trades {
		trades[i] = domainbehavior.InputTrade{
			ID:       uuid.Must(uuid.NewV7()),
			Symbol:   "EURUSD",
			Volume:   1,
			NetPL:    10,
			Outcome:  "win",
			OpenTime: from.Add(time.Duration(i) * time.Hour),
		}
	}
	writer := &behaviorProfileWriterStub{}
	svc := NewService(behaviorTradeReaderStub{trades: trades}, writer, writer)

	profile, err := svc.Analyze(context.Background(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), from, to)

	require.NoError(t, err)
	require.Equal(t, domainbehavior.ProfileStatusReady, profile.Status)
	require.Same(t, profile, writer.profile)
	require.Len(t, writer.findings, 1)
	require.Equal(t, domainbehavior.FindingSymbolConcentration, writer.findings[0].Code)
}

func TestServiceAnalyzeRejectsInvalidWindow(t *testing.T) {
	svc := NewService(behaviorTradeReaderStub{}, &behaviorProfileWriterStub{}, &behaviorProfileWriterStub{})
	_, err := svc.Analyze(context.Background(), uuid.Nil, uuid.Nil, time.Unix(2, 0), time.Unix(1, 0))
	require.ErrorIs(t, err, ErrInvalidWindow)
}
