<script setup lang="ts">
// /trades/:id — trade detail page (Phase 1 spec §23)
import { ref, onMounted, computed } from 'vue'
import { useTrades } from '~/composables/useTrades'
import type { Trade } from '@trading-journal/shared'

definePageMeta({ name: 'trade-detail' })

const route = useRoute()
const router = useRouter()
const { get } = useTrades()

const trade = ref<Trade | null>(null)
const loading = ref(true)
const errorMsg = ref<string | null>(null)

onMounted(async () => {
  try {
    trade.value = await get(route.params.id as string)
  } catch (err: any) {
    errorMsg.value = err?.message ?? 'Failed to load trade'
  } finally {
    loading.value = false
  }
})

const pl = computed(() => trade.value?.net_p_l ?? 0)
const plColor = computed(() => pl.value >= 0 ? 'success' : 'error')
const holding = computed(() => {
  if (!trade.value?.holding_seconds) return '—'
  const s = trade.value.holding_seconds
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  return `${(s / 3600).toFixed(1)}h`
})

function fmtMoney(n: number) {
  return n.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}
function fmtNum(n: number | null, d = 5) {
  return n == null ? '—' : n.toFixed(d)
}
</script>

<template>
  <v-container class="py-6">
    <v-btn variant="text" prepend-icon="mdi-arrow-left" @click="router.push('/trades')" class="mb-4">
      Back to trades
    </v-btn>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-alert v-else-if="errorMsg" type="error" :text="errorMsg" />

    <div v-else-if="trade">
      <div class="d-flex align-center justify-space-between mb-4">
        <div>
          <h1 class="text-h4">{{ trade.symbol }} <span class="text-medium-emphasis">{{ trade.direction.toUpperCase() }}</span></h1>
          <div class="text-body-2 text-medium-emphasis">
            Trade #{{ trade.id.slice(0, 8) }} · {{ trade.is_partial ? 'Partial close' : 'Full close' }}
          </div>
        </div>
        <v-chip :color="plColor" size="large" variant="flat">
          {{ pl >= 0 ? '+' : '' }}{{ fmtMoney(pl) }}
        </v-chip>
      </div>

      <v-row>
        <v-col cols="12" md="6">
          <v-card variant="outlined" class="pa-4">
            <h2 class="text-h6 mb-3">Trade</h2>
            <v-table density="compact">
              <tbody>
                <tr><td>Open Time</td><td>{{ new Date(trade.open_time).toLocaleString() }}</td></tr>
                <tr><td>Close Time</td><td>{{ trade.close_time ? new Date(trade.close_time).toLocaleString() : '—' }}</td></tr>
                <tr><td>Holding</td><td>{{ holding }}</td></tr>
                <tr><td>Volume</td><td>{{ trade.volume.toFixed(2) }}</td></tr>
                <tr><td>Open Price</td><td>{{ fmtNum(trade.open_price) }}</td></tr>
                <tr><td>Close Price</td><td>{{ fmtNum(trade.close_price) }}</td></tr>
                <tr><td>Commission</td><td>{{ fmtMoney(trade.commission) }}</td></tr>
                <tr><td>Swap</td><td>{{ fmtMoney(trade.swap) }}</td></tr>
              </tbody>
            </v-table>
          </v-card>
        </v-col>

        <v-col cols="12" md="6">
          <v-card variant="outlined" class="pa-4">
            <h2 class="text-h6 mb-3">P/L breakdown</h2>
            <v-table density="compact">
              <tbody>
                <tr><td>Gross Profit</td><td class="text-success">{{ fmtMoney(trade.gross_profit) }}</td></tr>
                <tr><td>Gross Loss</td><td class="text-error">{{ fmtMoney(trade.gross_loss) }}</td></tr>
                <tr><td>Net P/L</td><td :class="pl >= 0 ? 'text-success' : 'text-error'" class="font-weight-bold">
                  {{ fmtMoney(pl) }}
                </td></tr>
                <tr><td>Outcome</td><td><v-chip size="small" :color="plColor">{{ trade.outcome }}</v-chip></td></tr>
              </tbody>
            </v-table>
          </v-card>

          <v-card variant="outlined" class="pa-4 mt-4">
            <h2 class="text-h6 mb-3">Notes</h2>
            <v-alert type="info" variant="tonal">
              Journal entries, tags & screenshots land when Cloudinary + journal endpoints are wired.
            </v-alert>
          </v-card>
        </v-col>
      </v-row>
    </div>
  </v-container>
</template>