<script setup lang="ts">
// /dashboard — Phase 3 multi-account view.
// Shows tabs to switch between [All accounts] / [Account 1] / [Account 2] / …
// and per-account mini-cards below the global stats.
import { ref, computed, watch, onMounted } from 'vue'
import { useAuthStore } from '~/stores/auth'
import { useAnalytics } from '~/composables/useAnalytics'
import { useTrades } from '~/composables/useTrades'
import { useImports } from '~/composables/useImports'
import { useDashboardStore } from '~/stores/dashboard/dashboard'
import type { TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'dashboard' })

const auth = useAuthStore()
const router = useRouter()
const dash = useDashboardStore()
const { getOverview } = useAnalytics()
const { list } = useTrades()
const { listAccounts } = useImports()

const accounts = ref<TradingAccount[]>([])
const overview = ref<any>(null)
const recent = ref<any[]>([])
const equity = ref<any[]>([])
const loading = ref(true)

const overviewByAccount = ref<Record<string, any>>({})

const accountTabs = computed(() => [
  { value: null, label: 'All accounts' },
  ...accounts.value.map((a) => ({ value: a.id, label: a.display_name })),
])

const currentAccountID = computed(() => dash.selectedAccountId)

async function onLogout() {
  await auth.logout()
  await router.push('/login')
}

async function refresh() {
  loading.value = true
  try {
    const acc = await listAccounts()
    accounts.value = acc.accounts

    const accountID = currentAccountID.value
    const [ov, tr, eq] = await Promise.all([
      getOverview(accountID ?? undefined),
      list({ accountId: accountID ?? undefined, limit: 10 }),
      $fetch<{ equity_curve: any[] }>('/v1/analytics/equity-curve', {
        query: { limit: 200, account_id: accountID ?? undefined },
      }),
    ])
    overview.value = ov
    recent.value = tr.trades
    equity.value = eq.equity_curve

    // Per-account mini-cards: fetch overview for each account (Phase 3 §2.1)
    if (accounts.value.length > 0) {
      const tasks = accounts.value.map((a) =>
        getOverview(a.id)
          .then((o) => ({ id: a.id, overview: o }))
          .catch(() => ({ id: a.id, overview: null })),
      )
      const results = await Promise.all(tasks)
      const map: Record<string, any> = {}
      for (const r of results) {
        if (r.overview) map[r.id] = r.overview
      }
      overviewByAccount.value = map
    }
  } catch {
    // graceful fallback if no trades yet
  } finally {
    loading.value = false
  }
}

watch(currentAccountID, () => {
  // Re-fetch when the user changes the active tab. (Don't use deep — ref already.)
  refresh()
})

onMounted(refresh)

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

    <!-- Account selector (Phase 3 multi-account) -->
    <v-card v-if="accounts.length > 1" variant="outlined" class="mt-4 pa-2">
      <v-tabs
        :model-value="currentAccountID"
        @update:model-value="(v) => dash.setSelectedAccount(v as string | null)"
        show-arrows
      >
        <v-tab v-for="t in accountTabs" :key="String(t.value ?? 'all')" :value="t.value">
          {{ t.label }}
        </v-tab>
      </v-tabs>
    </v-card>

    <v-progress-circular v-if="loading" indeterminate color="primary" class="mt-6" />

    <template v-else-if="overview && overview.trade_count > 0">
      <!-- ── 4 stat tiles (now respect currentAccountID via BE) ── -->
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
        <v-table density="compact">
          <thead>
            <tr>
              <th>Symbol</th>
              <th>Direction</th>
              <th>Open</th>
              <th class="text-right">Net P/L</th>
            </tr>
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

      <EquityCurveChart :points="equity" class="mt-4" />
    </template>

    <v-card v-else class="mt-6 pa-8 text-center" variant="outlined">
      <v-icon size="56" color="medium-emphasis" class="mb-3">mdi-chart-line</v-icon>
      <div class="text-h6 mb-2">No trades yet</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        Upload an MT5 history file to see analytics.
      </div>
      <v-btn color="primary" to="/imports">Upload MT5 file</v-btn>
    </v-card>

    <!-- Per-account mini-cards (Phase 3 §2.1) -->
    <div v-if="accounts.length > 0" class="mt-6">
      <h2 class="text-h5 mb-3">Accounts</h2>
      <v-row>
        <v-col v-for="a in accounts" :key="a.id" cols="12" sm="6" md="4">
          <v-card
            variant="outlined"
            class="pa-4"
            :class="{ 'border-primary': currentAccountID === a.id }"
            @click="dash.setSelectedAccount(a.id)"
            style="cursor: pointer;"
          >
            <div class="d-flex justify-space-between align-center">
              <div class="font-weight-medium">{{ a.display_name }}</div>
              <v-chip size="x-small" color="primary" variant="tonal">
                {{ a.broker.toUpperCase() }}
              </v-chip>
            </div>
            <div class="text-caption text-medium-emphasis mb-2">
              {{ a.broker_account_id }} · {{ a.currency }} · {{ a.kind }}
            </div>
            <template v-if="overviewByAccount[a.id]">
              <div class="d-flex justify-space-between mt-2">
                <span class="text-body-2 text-medium-emphasis">Net P/L</span>
                <span class="font-weight-bold" :class="overviewByAccount[a.id].net_p_l >= 0 ? 'text-success' : 'text-error'">
                  {{ fmtMoney(overviewByAccount[a.id].net_p_l) }}
                </span>
              </div>
              <div class="d-flex justify-space-between">
                <span class="text-body-2 text-medium-emphasis">Trades</span>
                <span class="font-weight-bold">{{ overviewByAccount[a.id].trade_count }}</span>
              </div>
              <div class="d-flex justify-space-between">
                <span class="text-body-2 text-medium-emphasis">Win %</span>
                <span class="font-weight-bold">{{ fmtPct(overviewByAccount[a.id].win_rate) }}</span>
              </div>
            </template>
            <div v-else class="text-caption text-medium-emphasis">No trades yet.</div>
          </v-card>
        </v-col>
      </v-row>
    </div>

    <SyncStatusCard class="mt-6" />

    <v-card class="mt-6 pa-4 text-center" variant="outlined">
      <v-btn variant="outlined" color="error" prepend-icon="mdi-logout" @click="onLogout">
        Logout
      </v-btn>
    </v-card>
  </v-container>
</template>
