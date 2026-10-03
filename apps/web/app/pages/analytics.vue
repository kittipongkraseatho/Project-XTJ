<script setup lang="ts">
// /analytics — KPI dashboard (Phase 1 spec §30 MVP, expanded in Phase 3
// with session / EA / day-of-week / hour-of-day breakdowns).
import { ref, onMounted, computed } from 'vue'
import { useAnalytics } from '~/composables/useAnalytics'
import { useAnalysis, type SessionRow, type EARow, type DayOfWeekRow, type HourOfDayRow } from '~/composables/useAnalysis'
import { useImports } from '~/composables/useImports'
import { useDashboardStore } from '~/stores/dashboard/dashboard'
import type { TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'analytics' })

const { getOverview, getDailyPL, getSymbolBreakdown } = useAnalytics()
const { getSession, getEA, getDayOfWeek, getHourOfDay } = useAnalysis()
const { listAccounts } = useImports()
const dash = useDashboardStore()

const overview = ref<any>(null)
const dailyPL = ref<any[]>([])
const symbols = ref<any[]>([])
const sessions = ref<SessionRow[]>([])
const eas = ref<EARow[]>([])
const dow = ref<DayOfWeekRow[]>([])
const hod = ref<HourOfDayRow[]>([])

const accounts = ref<TradingAccount[]>([])
const loading = ref(true)
const errorMsg = ref<string | null>(null)

const accountTabs = computed(() => [
  { value: null, label: 'All accounts' },
  ...accounts.value.map((a) => ({ value: a.id, label: a.display_name })),
])

const now = new Date()
const monthAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)

async function refresh() {
  loading.value = true
  errorMsg.value = null
  try {
    if (accounts.value.length === 0) {
      try {
        const acc = await listAccounts()
        accounts.value = acc.accounts
      } catch {
        accounts.value = []
      }
    }

    const accountID = dash.selectedAccountId ?? undefined
    const [ov, dp, sb, ses, ea, dw, hr] = await Promise.all([
      getOverview(accountID),
      getDailyPL(monthAgo.toISOString().slice(0, 10), now.toISOString().slice(0, 10), accountID),
      getSymbolBreakdown(accountID),
      getSession(accountID ?? null),
      getEA(accountID ?? null),
      getDayOfWeek(accountID ?? null),
      getHourOfDay(accountID ?? null),
    ])
    overview.value = ov
    dailyPL.value = dp.daily
    symbols.value = sb.symbols
    sessions.value = ses.sessions
    eas.value = ea.eas
    dow.value = dw.weekdays
    hod.value = hr.hours
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load analytics'
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
</script>

<template>
  <v-container class="py-6">
    <h1 class="text-h4 mb-6">Analytics</h1>

    <!-- Account selector (Phase 3) -->
    <v-card v-if="accounts.length > 1" variant="outlined" class="mb-4 pa-2">
      <v-tabs
        :model-value="dash.selectedAccountId"
        @update:model-value="(v) => { dash.setSelectedAccount(v as string | null); refresh(); }"
        show-arrows
      >
        <v-tab v-for="t in accountTabs" :key="String(t.value ?? 'all')" :value="t.value">
          {{ t.label }}
        </v-tab>
      </v-tabs>
    </v-card>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-alert v-else-if="errorMsg" type="error" :text="errorMsg" />

    <template v-else-if="overview">
      <v-row>
        <v-col
          v-for="kpi in [
            { label: 'Net P/L', value: fmtMoney(overview.net_p_l), color: overview.net_p_l >= 0 ? 'success' : 'error' },
            { label: 'Win Rate', value: fmtPct(overview.win_rate), color: 'primary' },
            { label: 'Profit Factor', value: overview.profit_factor.toFixed(2), color: overview.profit_factor >= 1 ? 'success' : 'warning' },
            { label: 'Expectancy', value: fmtMoney(overview.expectancy), color: overview.expectancy >= 0 ? 'success' : 'error' },
            { label: 'Trade Count', value: overview.trade_count, color: 'accent' },
            { label: 'Avg Win', value: fmtMoney(overview.avg_win), color: 'success' },
            { label: 'Avg Loss', value: fmtMoney(overview.avg_loss), color: 'error' },
            { label: 'Gross Profit', value: fmtMoney(overview.gross_profit), color: 'success' },
          ]"
          :key="kpi.label"
          cols="6"
          sm="4"
          md="3"
        >
          <v-card variant="tonal" :color="kpi.color" class="pa-4">
            <div class="text-body-2 text-medium-emphasis">{{ kpi.label }}</div>
            <div class="text-h5 font-weight-bold mt-1">{{ kpi.value }}</div>
          </v-card>
        </v-col>
      </v-row>

      <!-- Phase 3 — 4 advanced analytics sections -->
      <v-row class="mt-4">
        <v-col cols="12" md="6">
          <SessionBreakdownChart :rows="sessions" />
        </v-col>
        <v-col cols="12" md="6">
          <DayOfWeekChart :rows="dow" />
        </v-col>
      </v-row>
      <v-row class="mt-4">
        <v-col cols="12">
          <HourOfDayHeatmap :rows="hod" />
        </v-col>
      </v-row>
      <v-row class="mt-4">
        <v-col cols="12">
          <EABreakdownTable :rows="eas" />
        </v-col>
      </v-row>

      <v-card class="pa-4 mt-6" variant="outlined">
        <h2 class="text-h6 mb-3">Daily P/L (last 30 days)</h2>
        <v-table v-if="dailyPL.length" density="compact">
          <thead>
            <tr><th>Date</th><th class="text-right">Trades</th><th class="text-right">Net P/L</th></tr>
          </thead>
          <tbody>
            <tr v-for="d in dailyPL" :key="d.trade_date">
              <td>{{ d.trade_date }}</td>
              <td class="text-right">{{ d.trade_count }}</td>
              <td class="text-right" :class="d.net_p_l >= 0 ? 'text-success' : 'text-error'">
                {{ fmtMoney(d.net_p_l) }}
              </td>
            </tr>
          </tbody>
        </v-table>
        <div v-else class="text-medium-emphasis text-body-2 py-6 text-center">
          No daily metrics — upload trades to populate.
        </div>
      </v-card>

      <v-card v-if="symbols.length" class="pa-4 mt-6" variant="outlined">
        <h2 class="text-h6 mb-3">Symbol Breakdown</h2>
        <v-table density="compact">
          <thead>
            <tr>
              <th>Symbol</th>
              <th class="text-right">Trades</th>
              <th class="text-right">Win %</th>
              <th class="text-right">Net P/L</th>
              <th class="text-right">Profit Factor</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in symbols" :key="s.symbol">
              <td class="font-weight-medium">{{ s.symbol }}</td>
              <td class="text-right">{{ s.trade_count }}</td>
              <td class="text-right">{{ fmtPct(s.win_rate) }}</td>
              <td class="text-right font-weight-medium" :class="s.net_p_l >= 0 ? 'text-success' : 'text-error'">
                {{ fmtMoney(s.net_p_l) }}
              </td>
              <td class="text-right">{{ s.profit_factor?.toFixed(2) }}</td>
            </tr>
          </tbody>
        </v-table>
      </v-card>
    </template>
  </v-container>
</template>