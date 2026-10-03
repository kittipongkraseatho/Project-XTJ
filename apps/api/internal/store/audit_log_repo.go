package store

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/gofrs/uuid"
)

// AuditEntry is the insert shape for audit_logs.
type AuditEntry struct {
	OccurredAt    *time.Time
	ActorUserID   *uuid.UUID
	ActorTenantID *uuid.UUID
	Action        string
	ResourceType  *string
	ResourceID    *uuid.UUID
	IP            *netip.Addr
	UserAgent     *string
	RequestID     *uuid.UUID
	Metadata      map[string]any
}

type AuditLogRepo struct{ pool DBTX }

func (r *AuditLogRepo) WithTx(tx DBTX) *AuditLogRepo { return &AuditLogRepo{pool: tx} }

// Insert appends an audit log row. Returns the id of the inserted row.
//
// Service layer is the ONLY caller — this is the append-only contract.
func (r *AuditLogRepo) Insert(ctx context.Context, e AuditEntry) error {
	meta, _ := json.Marshal(e.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_logs (occurred_at, actor_user_id, actor_tenant_id, action,
		                        resource_type, resource_id, ip, user_agent, request_id, metadata)
		VALUES (COALESCE($1, NOW()), $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.OccurredAt, e.ActorUserID, e.ActorTenantID, e.Action,
		e.ResourceType, e.ResourceID, e.IP, e.UserAgent, e.RequestID, meta)
	return err
}