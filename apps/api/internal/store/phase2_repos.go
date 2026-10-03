package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/broker_connection"
	"github.com/trading-journal/api/internal/domain/sync_run"
	"github.com/trading-journal/api/internal/tenant"
)

// ── BrokerConnectionRepo ──────────────────────────────────────────────

type BrokerConnectionRepo struct{ pool DBTX }

func (r *BrokerConnectionRepo) WithTx(tx DBTX) *BrokerConnectionRepo {
	return &BrokerConnectionRepo{pool: tx}
}

func (r *BrokerConnectionRepo) Create(ctx context.Context, c *broker_connection.BrokerConnection) error {
	meta, _ := json.Marshal(c.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO broker_connections (
			id, tenant_id, trading_account_id, provider, status,
			external_account_id, credentials_encrypted, metadata,
			sync_interval_seconds, last_sync_at, last_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at`,
		c.ID, c.TenantID, c.TradingAccountID, c.Provider, c.Status,
		c.ExternalAccountID, c.CredentialsEnc, meta,
		c.SyncIntervalSecs, c.LastSyncAt, c.LastError,
	)
	return row.Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (r *BrokerConnectionRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status broker_connection.Status, lastError *string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE broker_connections
		SET status = $2, last_error = $3, updated_at = NOW()
		WHERE id = $1`, id, status, lastError)
	return err
}

func (r *BrokerConnectionRepo) SetConnected(ctx context.Context, id uuid.UUID, externalAccountID string, metadata map[string]any) error {
	meta, _ := json.Marshal(metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE broker_connections
		SET status='connected', external_account_id=$2, last_error=NULL,
		    metadata=$3, updated_at=NOW()
		WHERE id=$1`, id, externalAccountID, meta)
	return err
}

func (r *BrokerConnectionRepo) MarkSynced(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE broker_connections
		SET last_sync_at = $2, last_error = NULL, updated_at = NOW()
		WHERE id = $1`, id, at)
	return err
}

func (r *BrokerConnectionRepo) GetByID(ctx context.Context, id uuid.UUID) (*broker_connection.BrokerConnection, error) {
	return scanBrokerConnection(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, provider, status,
		       external_account_id, credentials_encrypted, metadata,
		       last_sync_at, last_error, sync_interval_seconds, created_at, updated_at
		FROM broker_connections WHERE id = $1`, id))
}

// GetByIDByTenant is the tenant-scoped variant — returns ErrNotFound if the
// connection belongs to another tenant. Handlers MUST use this variant.
func (r *BrokerConnectionRepo) GetByIDByTenant(ctx context.Context, id uuid.UUID) (*broker_connection.BrokerConnection, error) {
	tenantID, err := tenant.TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, provider, status,
		       external_account_id, credentials_encrypted, metadata,
		       last_sync_at, last_error, sync_interval_seconds, created_at, updated_at
		FROM broker_connections
		WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return scanBrokerConnection(row)
}

func (r *BrokerConnectionRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]*broker_connection.BrokerConnection, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, trading_account_id, provider, status,
		       external_account_id, credentials_encrypted, metadata,
		       last_sync_at, last_error, sync_interval_seconds, created_at, updated_at
		FROM broker_connections
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*broker_connection.BrokerConnection
	for rows.Next() {
		c, err := scanBrokerConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListDueByTenant returns connected connections whose last_sync_at is older
// than sync_interval_seconds, or NULL (never synced).
func (r *BrokerConnectionRepo) ListDue(ctx context.Context, now time.Time, limit int) ([]*broker_connection.BrokerConnection, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, trading_account_id, provider, status,
		       external_account_id, credentials_encrypted, metadata,
		       last_sync_at, last_error, sync_interval_seconds, created_at, updated_at
		FROM broker_connections
		WHERE status = 'connected'
		  AND (last_sync_at IS NULL
		       OR last_sync_at + (sync_interval_seconds || ' seconds')::interval <= $1)
		ORDER BY last_sync_at NULLS FIRST, created_at
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*broker_connection.BrokerConnection
	for rows.Next() {
		c, err := scanBrokerConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *BrokerConnectionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM broker_connections WHERE id = $1`, id)
	return err
}

// scanBrokerConnection reads one connection row from any DBTX-scannable source.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanBrokerConnection(row rowScanner) (*broker_connection.BrokerConnection, error) {
	var c broker_connection.BrokerConnection
	var metaRaw []byte
	if err := row.Scan(
		&c.ID, &c.TenantID, &c.TradingAccountID, &c.Provider, &c.Status,
		&c.ExternalAccountID, &c.CredentialsEnc, &metaRaw,
		&c.LastSyncAt, &c.LastError, &c.SyncIntervalSecs, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(metaRaw, &c.Metadata)
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	return &c, nil
}

// ── SyncRunRepo ───────────────────────────────────────────────────────

type SyncRunRepo struct{ pool DBTX }

func (r *SyncRunRepo) WithTx(tx DBTX) *SyncRunRepo {
	return &SyncRunRepo{pool: tx}
}

func (r *SyncRunRepo) Create(ctx context.Context, s *sync_run.SyncRun) error {
	meta, _ := json.Marshal(s.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO sync_runs (
			id, tenant_id, broker_connection_id, status, trigger,
			started_at, finished_at,
			deals_fetched, deals_imported, deals_skipped,
			error, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at`,
		s.ID, s.TenantID, s.BrokerConnectionID, s.Status, s.Trigger,
		s.StartedAt, s.FinishedAt,
		s.DealsFetched, s.DealsImported, s.DealsSkipped,
		s.Error, meta)
	return row.Scan(&s.CreatedAt)
}

func (r *SyncRunRepo) MarkRunning(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sync_runs SET status='running', started_at=NOW()
		WHERE id=$1`, id)
	return err
}

func (r *SyncRunRepo) MarkSucceeded(ctx context.Context, id uuid.UUID, fetched, imported, skipped int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sync_runs
		SET status='succeeded', finished_at=NOW(),
		    deals_fetched=$2, deals_imported=$3, deals_skipped=$4, error=NULL
		WHERE id=$1`, id, fetched, imported, skipped)
	return err
}

func (r *SyncRunRepo) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sync_runs
		SET status='failed', finished_at=NOW(), error=$2
		WHERE id=$1`, id, errMsg)
	return err
}

func (r *SyncRunRepo) GetByID(ctx context.Context, id uuid.UUID) (*sync_run.SyncRun, error) {
	return scanSyncRun(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, broker_connection_id, status, trigger,
		       started_at, finished_at,
		       deals_fetched, deals_imported, deals_skipped,
		       error, metadata, created_at
		FROM sync_runs WHERE id=$1`, id))
}

// GetByIDByTenant is the tenant-scoped variant — returns ErrNotFound if the
// run belongs to another tenant. Handlers MUST use this variant.
func (r *SyncRunRepo) GetByIDByTenant(ctx context.Context, id uuid.UUID) (*sync_run.SyncRun, error) {
	tenantID, err := tenant.TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, broker_connection_id, status, trigger,
		       started_at, finished_at,
		       deals_fetched, deals_imported, deals_skipped,
		       error, metadata, created_at
		FROM sync_runs WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	return scanSyncRun(row)
}

// ListByConnection returns sync_runs for a connection, newest first.
// Caller MUST verify connection ownership before calling.
func (r *SyncRunRepo) ListByConnection(ctx context.Context, connectionID uuid.UUID, limit int) ([]*sync_run.SyncRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, broker_connection_id, status, trigger,
		       started_at, finished_at,
		       deals_fetched, deals_imported, deals_skipped,
		       error, metadata, created_at
		FROM sync_runs
		WHERE broker_connection_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, connectionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*sync_run.SyncRun
	for rows.Next() {
		s, err := scanSyncRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountByTenant returns the total sync_runs for the tenant (admin dashboard).
func (r *SyncRunRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	row := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sync_runs WHERE tenant_id=$1`, tenantID)
	err := row.Scan(&n)
	return n, err
}

func scanSyncRun(row rowScanner) (*sync_run.SyncRun, error) {
	var s sync_run.SyncRun
	var metaRaw []byte
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.BrokerConnectionID, &s.Status, &s.Trigger,
		&s.StartedAt, &s.FinishedAt,
		&s.DealsFetched, &s.DealsImported, &s.DealsSkipped,
		&s.Error, &metaRaw, &s.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(metaRaw, &s.Metadata)
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	return &s, nil
}