<script setup lang="ts">
// /analytics — KPI dashboard (Phase 1 spec §30 MVP)
import { ref, onMounted } from 'vue'
import { useAnalytics } from '~/composables/useAnalytics'

definePageMeta({ name: 'analytics' })

const { getOverview, getDailyPL } = useAnalytics()
const overview = ref<any>(null)
const dailyPL = ref<any[]>([])
const loading = ref(true)
const errorMsg = ref<string | null>(null)

const now = new Date()
const monthAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)

onMounted(async () => {
  try {
    const [ov, dp] = await Promise.all([
      getOverview(),
      getDailyPL(monthAgo.toISOString().slice(0, 10), now.toISOString().slice(0, 10)),
    ])
    overview.value = ov
    dailyPL.value = dp.daily
  } catch (err: any) {
    errorMsg.value = err?.message ?? 'Failed to load analytics'
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
    <h1 class="text-h4 mb-6">Analytics</h1>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-alert v-else-if="errorMsg" type="error" :text="errorMsg" />

    <template v-else-if="overview">
      <v-row>
        <v-col v-for="kpi in [
          { label: 'Net P/L', value: fmtMoney(overview.net_p_l), color: overview.net_p_l >= 0 ? 'success' : 'error' },
          { label: 'Win Rate', value: fmtPct(overview.win_rate), color: 'primary' },
          { label: 'Profit Factor', value: overview.profit_factor.toFixed(2), color: overview.profit_factor >= 1 ? 'success' : 'warning' },
          { label: 'Expectancy', value: fmtMoney(overview.expectancy), color: overview.expectancy >= 0 ? 'success' : 'error' },
          { label: 'Trade Count', value: overview.trade_count, color: 'accent' },
          { label: 'Avg Win', value: fmtMoney(overview.avg_win), color: 'success' },
          { label: 'Avg Loss', value: fmtMoney(overview.avg_loss), color: 'error' },
          { label: 'Gross Profit', value: fmtMoney(overview.gross_profit), color: 'success' },
        ]" :key="kpi.label" cols="6" sm="4" md="3">
          <v-card variant="tonal" :color="kpi.color" class="pa-4">
            <div class="text-body-2 text-medium-emphasis">{{ kpi.label }}</div>
            <div class="text-h5 font-weight-bold mt-1">{{ kpi.value }}</div>
          </v-card>
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
    </template>
  </v-container>
</template>