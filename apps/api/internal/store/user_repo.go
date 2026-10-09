package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/user"
)

// UserRepo wraps the users table.
//
// All methods accept either *pgxpool.Pool (via FromPool) or pgx.Tx.
type UserRepo struct{ pool DBTX }

// WithTx returns a new UserRepo bound to the given transaction.
func (r *UserRepo) WithTx(tx DBTX) *UserRepo { return &UserRepo{pool: tx} }

func (r *UserRepo) Create(ctx context.Context, u *user.User) error {
	meta, _ := json.Marshal(u.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, avatar_cloudinary_public_id, status, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.AvatarCloudinaryPublicID, u.Status, meta)
	if err := row.Scan(&u.CreatedAt, &u.UpdatedAt); err != nil {
		return err
	}
	if u.Metadata == nil {
		u.Metadata = map[string]any{}
	}
	return nil
}

func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	row := r.pool.QueryRow(ctx, userSelect+` WHERE id = $1 AND deleted_at IS NULL`, id)
	return scanUser(row)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	row := r.pool.QueryRow(ctx, userSelect+` WHERE email = $1 AND deleted_at IS NULL`, email)
	return scanUser(row)
}

func (r *UserRepo) UpdatePassword(ctx context.Context, id uuid.UUID, newHash string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id, newHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) UpdateDisplayName(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET display_name = $2, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrNotFound is the canonical "row not found" signal from the store layer.
// Service layer translates to domain.ErrUserNotFound / ErrTenantNotFound / etc.
var ErrNotFound = errors.New("store: not found")

// ── shared query fragments ──────────────────────────────────────────

const userSelect = `
SELECT id, email, password_hash, display_name, avatar_cloudinary_public_id,
       status, metadata, created_at, updated_at, deleted_at
FROM users`

func scanUser(row pgx.Row) (*user.User, error) {
	var u user.User
	var metaRaw []byte
	if err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.AvatarCloudinaryPublicID,
		&u.Status, &metaRaw, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &u.Metadata)
	}
	if u.Metadata == nil {
		u.Metadata = map[string]any{}
	}
	return &u, nil
}

// Helper to suppress unused-time import in builds that drop a method.
var _ = time.Time{}
