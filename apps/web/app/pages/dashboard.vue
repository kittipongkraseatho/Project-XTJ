<script setup lang="ts">
// /dashboard — Phase 1 real data (replaces Phase 0 placeholder)
import { ref, onMounted } from 'vue'
import { useAuthStore } from '~/stores/auth'
import { useAnalytics } from '~/composables/useAnalytics'
import { useTrades } from '~/composables/useTrades'

definePageMeta({ name: 'dashboard' })

const auth = useAuthStore()
const router = useRouter()
const { getOverview } = useAnalytics()
const { list } = useTrades()

const overview = ref<any>(null)
const recent = ref<any[]>([])
const equity = ref<any[]>([])
const loading = ref(true)

async function onLogout() {
  await auth.logout()
  await router.push('/login')
}

onMounted(async () => {
  try {
    const [ov, tr, eq] = await Promise.all([
      getOverview(),
      list({ limit: 10 }),
      $fetch<{ equity_curve: any[] }>('/v1/analytics/equity-curve', { query: { limit: 200 } }),
    ])
    overview.value = ov
    recent.value = tr.trades
    equity.value = eq.equity_curve
  } catch {
    // graceful fallback if no trades yet
  } finally {
    loading.value = false
  }
})

function fmtMoney(n: number) {
  return n.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}
function fmtPct(n: number) {
  return (n * 100).toFixed(1) + '%'
}
</script>

<template>
  <v-container class="py-6">
    <v-row>
      <v-col cols="12">
        <h1 class="text-h4 mb-1">Welcome, {{ auth.user?.display_name ?? 'trader' }} 👋</h1>
        <p class="text-body-2 text-medium-emphasis">
          Plan: <strong>{{ auth.plan?.name ?? '—' }}</strong>
          <span v-if="auth.plan"> · {{ auth.plan.currency }} {{ (auth.plan.price_cents / 100).toFixed(0) }}/{{ auth.plan.interval }}</span>
        </p>
      </v-col>
    </v-row>

    <v-progress-circular v-if="loading" indeterminate color="primary" class="mt-6" />

    <template v-else-if="overview && overview.trade_count > 0">
      <v-row class="mt-4">
        <v-col cols="6" sm="3">
          <v-card variant="tonal" :color="overview.net_p_l >= 0 ? 'success' : 'error'" class="pa-4">
            <div class="text-body-2 text-medium-emphasis">Net P/L</div>
            <div class="text-h5 font-weight-bold">{{ fmtMoney(overview.net_p_l) }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" sm="3">
          <v-card variant="tonal" color="primary" class="pa-4">
            <div class="text-body-2 text-medium-emphasis">Win Rate</div>
            <div class="text-h5 font-weight-bold">{{ fmtPct(overview.win_rate) }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" sm="3">
          <v-card variant="tonal" color="accent" class="pa-4">
            <div class="text-body-2 text-medium-emphasis">Profit Factor</div>
            <div class="text-h5 font-weight-bold">{{ overview.profit_factor.toFixed(2) }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" sm="3">
          <v-card variant="tonal" color="warning" class="pa-4">
            <div class="text-body-2 text-medium-emphasis">Trade Count</div>
            <div class="text-h5 font-weight-bold">{{ overview.trade_count }}</div>
          </v-card>
        </v-col>
      </v-row>

      <v-card class="mt-6 pa-4" variant="outlined">
        <div class="d-flex justify-space-between align-center mb-3">
          <h2 class="text-h6">Recent trades</h2>
          <v-btn variant="text" to="/trades">View all</v-btn>
        </div>
      </v-card>

      <EquityCurveChart :points="equity" class="mt-4" />
    </template>
        <v-table density="compact">
          <thead>
            <tr><th>Symbol</th><th>Direction</th><th>Open</th><th class="text-right">Net P/L</th></tr>
          </thead>
          <tbody>
            <tr v-for="t in recent" :key="t.id">
              <td>
                  <NuxtLink :to="`/trades/${t.id}`">{{ t.symbol }}</NuxtLink>
                </td>
              <td>{{ t.direction }}</td>
              <td class="text-caption">{{ new Date(t.open_time).toLocaleString() }}</td>
              <td class="text-right" :class="t.net_p_l >= 0 ? 'text-success' : 'text-error'">
                {{ fmtMoney(t.net_p_l) }}
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>
    </template>

    <v-card v-else class="mt-6 pa-8 text-center" variant="outlined">
      <v-icon size="56" color="medium-emphasis" class="mb-3">mdi-chart-line</v-icon>
      <div class="text-h6 mb-2">No trades yet</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        Upload an MT5 history file to see analytics.
      </div>
      <v-btn color="primary" to="/imports">Upload MT5 file</v-btn>
    </v-card>

    <SyncStatusCard class="mt-6" />

    <v-card class="mt-6 pa-4 text-center" variant="outlined">
      <v-btn variant="outlined" color="error" prepend-icon="mdi-logout" @click="onLogout">Logout</v-btn>
    </v-card>
  </v-container>
</template>