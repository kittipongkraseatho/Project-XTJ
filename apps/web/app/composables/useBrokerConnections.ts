// useBrokerConnections — Phase 2 broker connection + sync composables.

export interface BrokerConnection {
  id: string
  tenant_id: string
  trading_account_id: string
  provider: 'mock' | 'metaapi' | 'ctrader' | 'ib'
  status: 'pending' | 'connecting' | 'connected' | 'disconnected' | 'error'
  external_account_id: string | null
  metadata: Record<string, any>
  last_sync_at: string | null
  last_error: string | null
  sync_interval_seconds: number
  created_at: string
  updated_at: string
}

export interface SyncRun {
  id: string
  tenant_id: string
  broker_connection_id: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  trigger: 'manual' | 'scheduled' | 'reconnect'
  started_at: string | null
  finished_at: string | null
  deals_fetched: number
  deals_imported: number
  deals_skipped: number
  error: string | null
  metadata: Record<string, any>
  created_at: string
}

export function useBrokerConnections() {
  async function listConnections() {
    return await $fetch<{ connections: BrokerConnection[] }>('/v1/brokers/connections')
  }

  async function getConnection(id: string) {
    return await $fetch<BrokerConnection>(`/v1/brokers/connections/${id}`)
  }

  async function createConnection(input: {
    trading_account_id: string
    provider: string
    login?: string
    password?: string
    server?: string
    sync_interval_seconds?: number
  }) {
    return await $fetch<BrokerConnection>('/v1/brokers/connections', {
      method: 'POST',
      body: input,
    })
  }

  async function disconnect(id: string) {
    return await $fetch<{ id: string; status: string }>(`/v1/brokers/connections/${id}/disconnect`, {
      method: 'POST',
    })
  }

  async function deleteConnection(id: string) {
    return await $fetch<null>(`/v1/brokers/connections/${id}`, {
      method: 'DELETE',
    })
  }

  async function triggerSync(id: string, trigger: 'manual' | 'scheduled' | 'reconnect' = 'manual') {
    return await $fetch<{ sync_run_id: string; status: string; trigger: string }>(
      `/v1/brokers/connections/${id}/sync`,
      { method: 'POST', body: { trigger } },
    )
  }

  async function listSyncRuns(connectionId: string, limit = 20) {
    return await $fetch<{ sync_runs: SyncRun[] }>(`/v1/brokers/connections/${connectionId}/sync-runs`, {
      query: { limit },
    })
  }

  return {
    listConnections,
    getConnection,
    createConnection,
    disconnect,
    deleteConnection,
    triggerSync,
    listSyncRuns,
  }
}