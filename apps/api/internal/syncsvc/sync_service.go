// Package syncsvc — SyncService orchestrates broker → deals → trades.
//
// The headline invariant: a sync_run and a manual import_job both produce
// the same row shape and go through the same ON CONFLICT DO NOTHING dedup
// in dealRepo.Insert. A second sync on the same MockProvider (or any
// provider that emits stable provider_deal_id values) writes 0 new deals —
// which is exactly what lets us demo MetaApi reuse of the Phase 1 dedup.
//
// Lives in its own package (not service/) so worker.SyncWorker can import it
// without an import cycle: service.ImportService depends on worker.Pool.
package syncsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/broker_connection"
	"github.com/trading-journal/api/internal/domain/deal"
	"github.com/trading-journal/api/internal/domain/position"
	"github.com/trading-journal/api/internal/domain/sync_run"
	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/integration/broker"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/tenant"
)

// SyncService is the broker-sync use case.
//
// Lifecycle (HTTP): CreateBrokerConnection → Connect (Provider.Connect) →
// registered. SyncWorker polls BrokerConnectionRepo.ListDue and enqueues a
// sync_run; HTTP /sync enqueues manually. Each sync_run flows through
// RunSync which is the closure executed by the worker pool.
type SyncService struct {
	store    *store.Store
	registry *broker.Registry
	log      *slog.Logger
}

func NewSyncService(st *store.Store, reg *broker.Registry, log *slog.Logger) *SyncService {
	return &SyncService{store: st, registry: reg, log: log}
}

// CreateAndConnect does both the DB insert and the provider handshake in one
// step. The HTTP handler returns 201 with the resulting row (status='connected'
// on success) immediately — the worker takes over for any scheduled re-syncs.
//
// Credentials are NEVER persisted in plaintext — Mock ignores them; future
// providers must wrap them in AES-GCM via credentials_encrypted before storage.
func (s *SyncService) CreateAndConnect(ctx context.Context, in CreateConnectionInput) (*broker_connection.BrokerConnection, error) {
	if !in.Provider.IsValid() {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "unknown provider"})
	}
	// Verify the trading account belongs to this tenant (defense in depth — auth
	// middleware also checks membership).
	acc, err := s.store.TradingAccounts.GetByID(ctx, in.TradingAccountID)
	if err != nil {
		return nil, err
	}
	if acc.TenantID != in.TenantID {
		return nil, observability.ErrInsufficientRole
	}

	prov, err := s.registry.Get(in.Provider)
	if err != nil {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "provider not configured"})
	}

	res, err := prov.Connect(ctx, broker.ConnectRequest{
		Login:    in.Login,
		Password: in.Password,
		Server:   in.Server,
	})
	if err != nil {
		return nil, fmt.Errorf("provider connect: %w", err)
	}

	id, _ := uuid.NewV7()
	conn := &broker_connection.BrokerConnection{
		ID:                id,
		TenantID:          in.TenantID,
		TradingAccountID:  in.TradingAccountID,
		Provider:          in.Provider,
		Status:            broker_connection.StatusConnected,
		ExternalAccountID: &res.ExternalAccountID,
		Metadata:          res.Metadata,
		SyncIntervalSecs:  defaultIfZero(in.SyncIntervalSeconds, 60),
	}
	if err := s.store.BrokerConnections.Create(ctx, conn); err != nil {
		return nil, fmt.Errorf("create broker connection: %w", err)
	}
	return conn, nil
}

// EnqueueSync creates a sync_run row and submits a RunSync job to the worker
// pool. The HTTP handler returns the run id immediately; status updates
// arrive via polling /v1/brokers/connections/{id}/sync-runs.
func (s *SyncService) EnqueueSync(ctx context.Context, in EnqueueSyncInput) (*sync_run.SyncRun, error) {
	if !in.Trigger.IsValid() {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trigger"})
	}
	conn, err := s.store.BrokerConnections.GetByIDByTenant(ctx, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	if conn.Status != broker_connection.StatusConnected {
		return nil, observability.ErrValidation.WithDetails(map[string]any{
			"reason":  "connection not connected",
			"status":  string(conn.Status),
		})
	}

	id, _ := uuid.NewV7()
	run := &sync_run.SyncRun{
		ID:                 id,
		TenantID:           conn.TenantID,
		BrokerConnectionID: conn.ID,
		Status:             sync_run.StatusQueued,
		Trigger:            in.Trigger,
		Metadata:           map[string]any{},
	}
	if err := s.store.SyncRuns.Create(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// RunSync is the worker closure. It does NOT touch the tenant ctx — the tenant
// id is carried on sync_run.TenantID and re-injected at the top of the tx so
// any logging has it.
func (s *SyncService) RunSync(ctx context.Context, runID uuid.UUID) {
	startWall := time.Now()

	run, err := s.store.SyncRuns.GetByID(ctx, runID)
	if err != nil {
		s.log.Error("sync.run.load_failed", "run_id", runID, "err", err)
		return
	}
	if run.Status != sync_run.StatusQueued {
		s.log.Warn("sync.run.skip_not_queued", "run_id", runID, "status", string(run.Status))
		return
	}

	// Re-inject tenant ctx for downstream calls (defense in depth).
	ctx = tenant.WithTenantID(ctx, run.TenantID)

	if err := s.store.SyncRuns.MarkRunning(ctx, runID); err != nil {
		s.log.Error("sync.run.mark_running_failed", "run_id", runID, "err", err)
		return
	}

	conn, err := s.store.BrokerConnections.GetByID(ctx, run.BrokerConnectionID)
	if err != nil {
		s.failRun(ctx, runID, "connection_missing", err.Error())
		return
	}
	if conn.Status != broker_connection.StatusConnected || conn.ExternalAccountID == nil {
		s.failRun(ctx, runID, "connection_not_ready", "broker connection is not in connected state")
		return
	}

	prov, err := s.registry.Get(conn.Provider)
	if err != nil {
		s.failRun(ctx, runID, "provider_unconfigured", err.Error())
		return
	}

	if err := prov.Ping(ctx, *conn.ExternalAccountID); err != nil {
		_ = s.store.BrokerConnections.UpdateStatus(ctx, conn.ID, broker_connection.StatusError, ptrErrMsg("ping failed: "+err.Error()))
		s.failRun(ctx, runID, "ping_failed", err.Error())
		return
	}

	from := sevenDaysAgoIfZero(conn.LastSyncAt, startWall)
	deals, err := prov.FetchDeals(ctx, broker.FetchDealsRequest{
		ExternalAccountID: *conn.ExternalAccountID,
		From:              from,
		To:                startWall,
	})
	if err != nil {
		// Transient 5xx → broker_connection.error (sync continues retrying next interval)
		_ = s.store.BrokerConnections.UpdateStatus(ctx, conn.ID, broker_connection.StatusError, ptrErrMsg("fetch: "+err.Error()))
		s.failRun(ctx, runID, "fetch_failed", err.Error())
		return
	}

	inserted, skipped, err := s.writeDeals(ctx, conn, deals, runID)
	if err != nil {
		s.failRun(ctx, runID, "write_failed", err.Error())
		return
	}

	if err := s.store.BrokerConnections.MarkSynced(ctx, conn.ID, startWall); err != nil {
		s.log.Warn("sync.run.mark_synced_failed", "connection_id", conn.ID, "err", err)
	}
	if err := s.store.SyncRuns.MarkSucceeded(ctx, runID, len(deals), inserted, skipped); err != nil {
		s.log.Error("sync.run.mark_succeeded_failed", "run_id", runID, "err", err)
	}

	durationMS := time.Since(startWall).Milliseconds()
	s.log.Info("sync.run.completed",
		"run_id", runID,
		"connection_id", conn.ID,
		"tenant_id", run.TenantID,
		"deals_fetched", len(deals),
		"deals_imported", inserted,
		"deals_skipped", skipped,
		"duration_ms", durationMS,
	)
	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorTenantID:   &run.TenantID,
		Action:           "sync.completed",
		ResourceType:    strPtr("sync_run"),
		ResourceID:       &runID,
		Metadata: map[string]any{
			"fetched":  len(deals),
			"imported": inserted,
			"skipped":  skipped,
			"duration_ms": durationMS,
		},
	})
}

// writeDeals adapts broker.Deal → deal.Deal and creates Position+Trade rows for
// every newly-inserted deal. Skipped deals (dedup) do not generate new positions.
//
// We run inside a tx so a mid-batch failure rolls everything back.
func (s *SyncService) writeDeals(ctx context.Context, conn *broker_connection.BrokerConnection, deals []broker.Deal, runID uuid.UUID) (inserted, skipped int, err error) {
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		dealRepo := s.store.Deals.WithTx(tx)
		positionRepo := s.store.Positions.WithTx(tx)
		tradeRepo := s.store.Trades.WithTx(tx)

		acc, err := s.store.TradingAccounts.GetByID(ctx, conn.TradingAccountID)
		if err != nil {
			return fmt.Errorf("load trading account: %w", err)
		}
		if !acc.IsActive {
			return observability.ErrValidation.WithDetails(map[string]any{"reason": "trading account inactive"})
		}

		for _, bd := range deals {
			d := brokerDealToDealRow(bd, conn, runID)
			ok, err := dealRepo.Insert(ctx, d)
			if err != nil {
				return fmt.Errorf("insert deal %s: %w", bd.ProviderDealID, err)
			}
			if !ok {
				skipped++
				continue
			}
			inserted++

			// Mock produces 1 closed deal per position. Real MetaApi/cTrader
			// adapters may aggregate multiple deals into one Position; that
			// logic will live in the provider-specific adapter (Phase 4+).
			posID, _ := uuid.NewV7()
			openPx := float64(bd.OpenPrice) / 100_000
			closePx := float64(bd.ClosePrice) / 100_000
			volume := float64(bd.Volume) / 100_000
			profit := float64(bd.Profit) / 100
			commission := float64(bd.Commission) / 100
			swap := float64(bd.Swap) / 100
			openT := bd.OpenTime
			closeT := bd.CloseTime
			holdingSec := int64(closeT.Sub(openT).Seconds())
			dir := directionToPosition(bd.Direction)

			pos := &position.Position{
				ID:                 posID,
				TenantID:           conn.TenantID,
				TradingAccountID:   conn.TradingAccountID,
				ProviderPositionID: bd.ProviderDealID,
				Direction:          dir,
				Symbol:             bd.Symbol,
				Volume:             volume,
				OpenPrice:          openPx,
				ClosePrice:         &closePx,
				OpenTime:           openT,
				CloseTime:          &closeT,
				State:              position.StateClosed,
				RealizedPL:         profit,
				Commission:         commission,
				Swap:               swap,
				Metadata:           map[string]any{"source": "broker", "provider": string(conn.Provider)},
			}
			if err := positionRepo.Insert(ctx, pos); err != nil {
				return fmt.Errorf("insert position: %w", err)
			}

			outcome := trade.OutcomeBreakeven
			if profit > 0 {
				outcome = trade.OutcomeWin
			} else if profit < 0 {
				outcome = trade.OutcomeLoss
			}
			var gp, gl float64
			if profit > 0 {
				gp = profit
			} else {
				gl = profit
			}
			tr := &trade.Trade{
				ID:               uuid.Must(uuid.NewV7()),
				TenantID:         conn.TenantID,
				UserID:           acc.UserID,
				TradingAccountID: conn.TradingAccountID,
				PositionID:       &posID,
				Symbol:           bd.Symbol,
				Direction:        directionToTrade(bd.Direction),
				Volume:           volume,
				OpenPrice:        openPx,
				ClosePrice:       &closePx,
				OpenTime:         openT,
				CloseTime:        &closeT,
				HoldingSeconds:   &holdingSec,
				GrossProfit:      gp,
				GrossLoss:        gl,
				NetPL:            profit + commission + swap,
				Commission:       commission,
				Swap:             swap,
				Outcome:          outcome,
				IsPartial:        false,
				Metadata:         map[string]any{"source": "broker", "provider": string(conn.Provider)},
			}
			if err := tradeRepo.Insert(ctx, tr); err != nil {
				return fmt.Errorf("insert trade: %w", err)
			}
		}
		return nil
	})
	return inserted, skipped, err
}

func (s *SyncService) failRun(ctx context.Context, runID uuid.UUID, code, msg string) {
	if err := s.store.SyncRuns.MarkFailed(ctx, runID, msg); err != nil {
		s.log.Error("sync.run.mark_failed_failed", "run_id", runID, "err", err)
	}
	s.log.Warn("sync.run.failed", "run_id", runID, "code", code, "err", msg)
}

// ── DTOs and helpers ──────────────────────────────────────────────────

type CreateConnectionInput struct {
	TenantID          uuid.UUID
	TradingAccountID  uuid.UUID
	Provider          broker_connection.Provider
	Login             string
	Password          string
	Server            string
	SyncIntervalSeconds int
}

type EnqueueSyncInput struct {
	TenantID    uuid.UUID
	ConnectionID uuid.UUID
	Trigger     sync_run.Trigger
}

func defaultIfZero(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func sevenDaysAgoIfZero(t *time.Time, ref time.Time) time.Time {
	if t == nil {
		return ref.Add(-7 * 24 * time.Hour)
	}
	return *t
}

func ptrErrMsg(s string) *string { return &s }
func strPtr(s string) *string   { return &s }

func brokerDealToDealRow(bd broker.Deal, conn *broker_connection.BrokerConnection, runID uuid.UUID) *deal.Deal {
	id, _ := uuid.NewV7()
	t := deal.TypeBuy
	if bd.Direction == "sell" {
		t = deal.TypeSell
	}
	return &deal.Deal{
		ID:               id,
		TenantID:         conn.TenantID,
		TradingAccountID: conn.TradingAccountID,
		ImportJobID:      nil, // broker-sourced deals do NOT belong to an import_job row
		ProviderDealID:   bd.ProviderDealID,
		Symbol:           bd.Symbol,
		Type:             t,
		EntryType:        deal.EntryEntry, // broker.Deal is a closed position (open+close)
		Volume:           float64(bd.Volume) / 100_000, // micro lots → lots
		Price:            float64(bd.OpenPrice) / 100_000,
		Commission:       float64(bd.Commission) / 100,
		Swap:             float64(bd.Swap) / 100,
		Profit:           float64(bd.Profit) / 100,
		ExecutedAt:       bd.OpenTime,
		Metadata:         map[string]any{"source": "broker", "sync_run_id": runID.String(), "provider": string(conn.Provider)},
	}
}

func directionToTrade(s string) trade.Direction {
	if s == "sell" {
		return trade.DirectionShort
	}
	return trade.DirectionLong
}

func directionToPosition(s string) position.Direction {
	if s == "sell" {
		return position.DirectionShort
	}
	return position.DirectionLong
}