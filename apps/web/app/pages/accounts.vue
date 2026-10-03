<script setup lang="ts">
// /accounts — list trading accounts + broker connections (Phase 2).
import { ref, onMounted, computed } from 'vue'
import { useImports } from '~/composables/useImports'
import { useBrokerConnections, type BrokerConnection, type SyncRun } from '~/composables/useBrokerConnections'
import type { TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'accounts' })

const { listAccounts } = useImports()
const { listConnections, createConnection, disconnect, deleteConnection, triggerSync, listSyncRuns } = useBrokerConnections()

const accounts = ref<TradingAccount[]>([])
const connections = ref<BrokerConnection[]>([])
const loading = ref(true)

const showConnectDialog = ref(false)
const newProvider = ref('mock')
const newAccountID = ref('')
const newServer = ref('MockBroker-Server')
const newInterval = ref(60)
const connectError = ref<string | null>(null)
const connecting = ref(false)

const syncBusy = ref<Record<string, boolean>>({})
const showHistoryDialog = ref(false)
const historyConnection = ref<BrokerConnection | null>(null)
const historyRuns = ref<SyncRun[]>([])
const historyLoading = ref(false)

onMounted(async () => {
  await refresh()
})

async function refresh() {
  loading.value = true
  try {
    const [a, c] = await Promise.all([listAccounts(), listConnections()])
    accounts.value = a.accounts
    connections.value = c.connections
  } finally {
    loading.value = false
  }
}

const accountsByID = computed(() => {
  const m = new Map<string, TradingAccount>()
  for (const a of accounts.value) m.set(a.id, a)
  return m
})

function accountLabel(accountID: string): string {
  const a = accountsByID.value.get(accountID)
  return a ? `${a.display_name} (${a.broker_account_id})` : accountID
}

function statusColor(s: string): string {
  if (s === 'connected') return 'success'
  if (s === 'error') return 'error'
  if (s === 'disconnected') return 'warning'
  return 'grey'
}

function relTime(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  const sec = Math.floor((Date.now() - d.getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

async function openConnectDialog() {
  if (accounts.value.length === 0) {
    connectError.value = 'Create a trading account first.'
    return
  }
  newAccountID.value = accounts.value[0].id
  showConnectDialog.value = true
  connectError.value = null
}

async function submitConnect() {
  if (!newAccountID.value) {
    connectError.value = 'Select a trading account.'
    return
  }
  connecting.value = true
  connectError.value = null
  try {
    await createConnection({
      trading_account_id: newAccountID.value,
      provider: newProvider.value,
      login: 'demo-login',
      password: 'demo-password',
      server: newServer.value,
      sync_interval_seconds: newInterval.value,
    })
    showConnectDialog.value = false
    await refresh()
  } catch (err: any) {
    connectError.value = err?.data?.error?.message || 'Failed to connect.'
  } finally {
    connecting.value = false
  }
}

async function onTriggerSync(conn: BrokerConnection) {
  syncBusy.value = { ...syncBusy.value, [conn.id]: true }
  try {
    await triggerSync(conn.id)
    await refresh()
  } finally {
    syncBusy.value = { ...syncBusy.value, [conn.id]: false }
  }
}

async function onDisconnect(conn: BrokerConnection) {
  await disconnect(conn.id)
  await refresh()
}

async function onDelete(conn: BrokerConnection) {
  if (!confirm(`Delete broker connection "${conn.provider}" for ${accountLabel(conn.trading_account_id)}?`)) return
  await deleteConnection(conn.id)
  await refresh()
}

async function openHistory(conn: BrokerConnection) {
  historyConnection.value = conn
  showHistoryDialog.value = true
  historyLoading.value = true
  try {
    const res = await listSyncRuns(conn.id, 50)
    historyRuns.value = res.sync_runs
  } finally {
    historyLoading.value = false
  }
}
</script>

<template>
  <v-container class="py-6">
    <h1 class="text-h4 mb-4">Trading Accounts</h1>

    <v-progress-circular v-if="loading && !accounts.length" indeterminate color="primary" />

    <v-card v-else-if="!accounts.length" variant="outlined" class="pa-12 text-center">
      <v-icon size="48" color="medium-emphasis" class="mb-2">mdi-account-multiple</v-icon>
      <div class="text-h6 mb-2">No trading accounts yet</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        Create your first account to start importing MT5 history.
      </div>
      <v-btn color="primary" to="/imports">Get started</v-btn>
    </v-card>

    <v-table v-else density="comfortable">
      <thead>
        <tr>
          <th>Display Name</th>
          <th>Broker</th>
          <th>Account ID</th>
          <th>Currency</th>
          <th>Kind</th>
          <th>Created</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="a in accounts" :key="a.id">
          <td class="font-weight-medium">{{ a.display_name }}</td>
          <td>{{ a.broker.toUpperCase() }}</td>
          <td>{{ a.broker_account_id }}</td>
          <td>{{ a.currency }}</td>
          <td><v-chip size="small" :color="a.kind === 'real' ? 'primary' : 'warning'">{{ a.kind }}</v-chip></td>
          <td class="text-caption">{{ new Date(a.created_at).toLocaleDateString() }}</td>
          <td>
            <v-btn variant="text" size="small" :to="`/imports?account_id=${a.id}`">Import</v-btn>
          </td>
        </tr>
      </tbody>
    </v-table>

    <!-- ── Broker connections (Phase 2) ─────────────────────────────── -->
    <div class="d-flex align-center justify-space-between mt-8 mb-3">
      <h2 class="text-h5">Broker Connections</h2>
      <v-btn color="primary" prepend-icon="mdi-plus" :disabled="!accounts.length" @click="openConnectDialog">
        Connect Broker
      </v-btn>
    </div>

    <v-progress-circular v-if="loading && !connections.length && accounts.length" indeterminate color="primary" />

    <v-card v-else-if="!connections.length" variant="outlined" class="pa-8 text-center">
      <v-icon size="40" color="medium-emphasis" class="mb-2">mdi-link-off</v-icon>
      <div class="text-body-2 text-medium-emphasis">
        No broker connections yet. Click <strong>Connect Broker</strong> to start.
      </div>
    </v-card>

    <v-card v-else variant="outlined">
      <v-list lines="three">
        <v-list-item v-for="c in connections" :key="c.id">
          <template #prepend>
            <v-avatar :color="statusColor(c.status)" size="32">
              <v-icon color="white">mdi-link-variant</v-icon>
            </v-avatar>
          </template>
          <v-list-item-title>
            {{ c.provider.toUpperCase() }} — {{ accountLabel(c.trading_account_id) }}
          </v-list-item-title>
          <v-list-item-subtitle>
            <v-chip size="x-small" :color="statusColor(c.status)" variant="flat" class="mr-2">
              {{ c.status }}
            </v-chip>
            Last sync: {{ relTime(c.last_sync_at) }}
            <span v-if="c.last_error" class="text-error ml-2">— {{ c.last_error }}</span>
          </v-list-item-subtitle>
          <template #append>
            <v-btn variant="text" size="small" :loading="syncBusy[c.id]" @click="onTriggerSync(c)">Sync now</v-btn>
            <v-btn variant="text" size="small" @click="openHistory(c)">History</v-btn>
            <v-menu>
                <template #activator="{ props }">
                  <v-btn icon="mdi-dots-vertical" variant="text" size="small" v-bind="props" />
                </template>
                <v-list density="compact">
                  <v-list-item v-if="c.status === 'connected'" @click="onDisconnect(c)">
                    <v-list-item-title>Disconnect</v-list-item-title>
                  </v-list-item>
                  <v-list-item @click="onDelete(c)">
                    <v-list-item-title class="text-error">Delete</v-list-item-title>
                  </v-list-item>
                </v-list>
              </v-menu>
          </template>
        </v-list-item>
      </v-list>
    </v-card>

    <!-- ── Connect dialog ───────────────────────────────────────────── -->
    <v-dialog v-model="showConnectDialog" max-width="560">
      <v-card>
        <v-card-title>Connect Broker</v-card-title>
        <v-card-text>
          <v-select
            v-model="newAccountID"
            :items="accounts"
            item-title="display_name"
            item-value="id"
            density="compact"
            label="Trading account"
            class="mb-2"
          />
          <v-select
            v-model="newProvider"
            :items="[{ title: 'Mock Broker (Phase 2 demo)', value: 'mock' }]"
            item-title="title"
            item-value="value"
            density="compact"
            label="Provider"
            class="mb-2"
            hint="Mock provider generates deterministic fake trades. Real MetaApi (Phase 4) will appear once configured."
            persistent-hint
          />
          <v-text-field v-model="newServer" label="Server name (mock-only)" density="compact" class="mb-2" />
          <v-text-field
            v-model.number="newInterval"
            label="Sync interval (seconds)"
            type="number"
            density="compact"
            :min="10"
            class="mb-2"
          />
          <v-alert v-if="connectError" type="error" variant="tonal" class="mb-2">{{ connectError }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showConnectDialog = false">Cancel</v-btn>
          <v-btn color="primary" :loading="connecting" @click="submitConnect">Connect</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- ── Sync history dialog ──────────────────────────────────────── -->
    <v-dialog v-model="showHistoryDialog" max-width="720">
      <v-card>
        <v-card-title>
          Sync history
          <span v-if="historyConnection" class="text-caption text-medium-emphasis">
            — {{ historyConnection.provider.toUpperCase() }} · {{ accountLabel(historyConnection.trading_account_id) }}
          </span>
        </v-card-title>
        <v-card-text>
          <v-progress-circular v-if="historyLoading" indeterminate color="primary" />
          <div v-else-if="!historyRuns.length" class="text-medium-emphasis text-body-2 text-center pa-6">
            No sync runs yet. Trigger one with "Sync now".
          </div>
          <v-table v-else density="compact">
            <thead>
              <tr>
                <th>Started</th>
                <th>Trigger</th>
                <th>Status</th>
                <th>Fetched</th>
                <th>Imported</th>
                <th>Skipped</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in historyRuns" :key="r.id">
                <td class="text-caption">{{ relTime(r.started_at || r.created_at) }}</td>
                <td>{{ r.trigger }}</td>
                <td>
                  <v-chip size="x-small" :color="r.status === 'succeeded' ? 'success' : r.status === 'failed' ? 'error' : 'grey'">
                    {{ r.status }}
                  </v-chip>
                </td>
                <td>{{ r.deals_fetched }}</td>
                <td>{{ r.deals_imported }}</td>
                <td>{{ r.deals_skipped }}</td>
                <td class="text-caption text-error">{{ r.error || '' }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showHistoryDialog = false">Close</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>