<script setup lang="ts">
// /trades — paginated list with filters
import { ref, onMounted, computed } from 'vue'
import { useTrades } from '~/composables/useTrades'
import type { Trade } from '@trading-journal/shared'

definePageMeta({ name: 'trades' })

const { list } = useTrades()

const trades = ref<Trade[]>([])
const total = ref(0)
const loading = ref(true)
const offset = ref(0)
const limit = 50

async function refresh() {
  loading.value = true
  try {
    const res = await list({ limit, offset: offset.value })
    trades.value = res.trades
    total.value = res.total
  } finally {
    loading.value = false
  }
}

onMounted(refresh)

function fmtMoney(n: number) {
  return n.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}
function fmtPct(n: number) {
  return (n * 100).toFixed(1) + '%'
}
function outcomeColor(o: string) {
  switch (o) {
    case 'win': return 'success'
    case 'loss': return 'error'
    default: return 'medium-emphasis'
  }
}
</script>

<template>
  <v-container class="py-6">
    <div class="d-flex align-center justify-space-between mb-4">
      <h1 class="text-h4">Trades</h1>
      <span class="text-body-2 text-medium-emphasis">{{ total }} total</span>
    </div>

    <v-card v-if="loading" class="pa-12 text-center">
      <v-progress-circular indeterminate color="primary" />
    </v-card>

    <v-card v-else-if="!trades.length" variant="outlined" class="pa-12 text-center">
      <v-icon size="48" color="medium-emphasis" class="mb-2">mdi-chart-bar</v-icon>
      <div class="text-h6 mb-2">No trades yet</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        Upload an MT5 history file to populate your trades.
      </div>
      <v-btn color="primary" to="/imports">Upload file</v-btn>
    </v-card>

    <v-table v-else density="comfortable">
      <thead>
        <tr>
          <th>Symbol</th>
          <th>Direction</th>
          <th>Open</th>
          <th>Close</th>
          <th class="text-right">Volume</th>
          <th class="text-right">Open Price</th>
          <th class="text-right">Net P/L</th>
          <th class="text-center">Outcome</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="t in trades" :key="t.id">
          <td>
            <NuxtLink :to="`/trades/${t.id}`" class="font-weight-medium">{{ t.symbol }}</NuxtLink>
          </td>
          <td>{{ t.direction }}</td>
          <td class="text-caption">{{ new Date(t.open_time).toLocaleString() }}</td>
          <td class="text-caption">{{ t.close_time ? new Date(t.close_time).toLocaleString() : '—' }}</td>
          <td class="text-right">{{ t.volume.toFixed(2) }}</td>
          <td class="text-right">{{ t.open_price.toFixed(5) }}</td>
          <td class="text-right font-weight-medium" :class="t.net_p_l >= 0 ? 'text-success' : 'text-error'">
            {{ fmtMoney(t.net_p_l) }}
          </td>
          <td class="text-center">
            <v-chip :color="outcomeColor(t.outcome)" size="small">{{ t.outcome }}</v-chip>
          </td>
        </tr>
      </tbody>
    </v-table>

    <div v-if="trades.length" class="d-flex justify-center mt-4">
      <v-pagination
        v-model="offset"
        :length="Math.ceil(total / limit)"
        :total-visible="7"
        @update:model-value="refresh"
      />
    </div>
  </v-container>
</template>