<script setup lang="ts">
// SyncStatusCard — dashboard widget showing broker connection state.
// Phase 2 — wired to /v1/brokers/connections.
import { ref, onMounted, computed } from 'vue'
import { useBrokerConnections, type BrokerConnection } from '~/composables/useBrokerConnections'

const { listConnections, triggerSync } = useBrokerConnections()
const connections = ref<BrokerConnection[]>([])
const loading = ref(true)
const syncing = ref<Record<string, boolean>>({})

const stats = computed(() => ({
  total: connections.value.length,
  connected: connections.value.filter((c) => c.status === 'connected').length,
  error: connections.value.filter((c) => c.status === 'error').length,
}))

async function refresh() {
  try {
    const res = await listConnections()
    connections.value = res.connections
  } finally {
    loading.value = false
  }
}

async function onSync(conn: BrokerConnection) {
  syncing.value = { ...syncing.value, [conn.id]: true }
  try {
    await triggerSync(conn.id)
    await refresh()
  } finally {
    syncing.value = { ...syncing.value, [conn.id]: false }
  }
}

function relTime(iso: string | null): string {
  if (!iso) return 'never'
  const sec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

function statusColor(s: string): string {
  if (s === 'connected') return 'success'
  if (s === 'error') return 'error'
  if (s === 'disconnected') return 'warning'
  return 'grey'
}

onMounted(refresh)
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <div class="d-flex align-center justify-space-between mb-2">
      <h2 class="text-h6">Broker Sync</h2>
      <v-btn variant="text" size="small" to="/accounts">Manage</v-btn>
    </div>

    <v-progress-circular v-if="loading" indeterminate color="primary" size="20" />

    <div v-else-if="stats.total === 0" class="text-body-2 text-medium-emphasis py-2">
      No broker connections.
      <NuxtLink to="/accounts">Connect one</NuxtLink>
      to start auto-syncing trades.
    </div>

    <div v-else>
      <div class="d-flex gap-3 mb-3">
        <v-chip size="small" variant="tonal" color="success">{{ stats.connected }} connected</v-chip>
        <v-chip v-if="stats.error" size="small" variant="tonal" color="error">{{ stats.error }} error</v-chip>
      </div>

      <v-list density="compact" class="bg-transparent">
        <v-list-item v-for="c in connections" :key="c.id">
          <template #prepend>
            <v-avatar :color="statusColor(c.status)" size="24">
              <v-icon size="14" color="white">mdi-link-variant</v-icon>
            </v-avatar>
          </template>
          <v-list-item-title class="text-body-2">
            {{ c.provider.toUpperCase() }} — last sync {{ relTime(c.last_sync_at) }}
          </v-list-item-title>
          <v-list-item-subtitle v-if="c.last_error" class="text-error text-caption">
            {{ c.last_error }}
          </v-list-item-subtitle>
          <template #append>
            <v-btn
              v-if="c.status === 'connected'"
              variant="text"
              size="x-small"
              :loading="syncing[c.id]"
              @click="onSync(c)"
            >
              Sync
            </v-btn>
          </template>
        </v-list-item>
      </v-list>
    </div>
  </v-card>
</template>
