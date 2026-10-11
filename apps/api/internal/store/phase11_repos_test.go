package store

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type phase11RecordingDB struct {
	execSQL  []string
	querySQL []string
	rowErr   error
}

func (f *phase11RecordingDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *phase11RecordingDB) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	f.querySQL = append(f.querySQL, sql)
	return nil, errors.New("query rows not configured")
}

func (f *phase11RecordingDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.querySQL = append(f.querySQL, sql)
	return phase11Row{err: f.rowErr}
}

type phase11Row struct{ err error }

func (r phase11Row) Scan(_ ...any) error { return r.err }

func TestPhase11RepositoriesScopeTenantAndAccount(t *testing.T) {
	db := &phase11RecordingDB{rowErr: pgx.ErrNoRows}
	tenantID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())
	id := uuid.Must(uuid.NewV7())

	_, err := (&BehaviorProfileRepo{pool: db}).GetLatest(context.Background(), tenantID, accountID)
	require.ErrorIs(t, err, ErrNotFound)
	require.Contains(t, db.querySQL[0], "tenant_id = $1")
	require.Contains(t, db.querySQL[0], "trading_account_id = $2")

	_, err = (&StrategyRepo{pool: db}).GetByIDByTenant(context.Background(), tenantID, id)
	require.ErrorIs(t, err, ErrNotFound)
	require.Contains(t, db.querySQL[1], "tenant_id = $2")
}

func TestTradeStrategyVersionLinkScopesTenant(t *testing.T) {
	db := &phase11RecordingDB{}
	err := (&TradeRepo{pool: db}).LinkStrategyVersion(context.Background(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))

	require.NoError(t, err)
	require.Contains(t, db.execSQL[0], "WHERE id = $1 AND tenant_id = $2")
}
