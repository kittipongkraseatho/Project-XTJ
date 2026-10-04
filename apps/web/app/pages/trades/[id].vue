<script setup lang="ts">
// /trades/:id — trade detail page (Phase 1 spec §23) — extended w/ journal, tags, screenshots
// + Phase 6 AI Analysis panel.
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { useTrades } from '~/composables/useTrades'
import { useAnalysis, type Analysis, type Usage } from '~/composables/useAnalysis'
import type { Trade } from '@trading-journal/shared'

definePageMeta({ name: 'trade-detail' })

const route = useRoute()
const router = useRouter()
const { get } = useTrades()
const { analyze, getLatest, getUsage } = useAnalysis()

const trade = ref<Trade | null>(null)
const loading = ref(true)
const errorMsg = ref<string | null>(null)

// AI panel state
const analysis = ref<Analysis | null>(null)
const usage = ref<Usage | null>(null)
const analysisBusy = ref(false)
const analysisErr = ref<string | null>(null)
let pollHandle: ReturnType<typeof setInterval> | null = null

async function refreshUsage() {
  try {
    usage.value = await getUsage()
  } catch {
    // Non-fatal.
  }
}

async function loadExisting() {
  try {
    analysis.value = await getLatest(route.params.id as string)
  } catch {
    analysis.value = null
  }
}

async function startPolling() {
  stopPolling()
  pollHandle = setInterval(async () => {
    if (!analysis.value) return
    if (analysis.value.status !== 'pending' && analysis.value.status !== 'generating') return
    try {
      analysis.value = await getLatest(route.params.id as string)
    } catch {
      // keep polling; FE handles transient errors
    }
  }, 3000)
}

function stopPolling() {
  if (pollHandle) {
    clearInterval(pollHandle)
    pollHandle = null
  }
}

async function onAnalyze() {
  if (!trade.value) return
  analysisBusy.value = true
  analysisErr.value = null
  try {
    analysis.value = await analyze(trade.value.id)
    await refreshUsage()
    if (analysis.value.status === 'pending' || analysis.value.status === 'generating') {
      startPolling()
    }
  } catch (err: any) {
    if (err?.statusCode === 402) {
      analysisErr.value = 'Daily budget exhausted. Try again tomorrow.'
    } else {
      analysisErr.value = err?.data?.error?.message ?? err?.message ?? 'Analyze failed'
    }
  } finally {
    analysisBusy.value = false
  }
}

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
function providerLabel(p: string): string {
  if (p === 'openai') return 'OpenAI'
  if (p === 'anthropic') return 'Anthropic'
  return 'Stub (local)'
}
function statusColor(s: string): string {
  switch (s) {
    case 'success': return 'success'
    case 'pending': return 'info'
    case 'generating': return 'info'
    case 'failed': return 'error'
    default: return 'grey'
  }
}

onMounted(async () => {
  try {
    trade.value = await get(route.params.id as string)
    await Promise.all([loadExisting(), refreshUsage()])
    if (analysis.value && (analysis.value.status === 'pending' || analysis.value.status === 'generating')) {
      startPolling()
    }
  } catch (err: any) {
    errorMsg.value = err?.message ?? 'Failed to load trade'
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(stopPolling)
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

          <v-card variant="outlined" class="pa-4 mt-4">
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
        </v-col>

        <v-col cols="12" md="6">
          <v-card variant="outlined" class="pa-4 mb-4">
            <div class="d-flex align-center mb-2">
              <h2 class="text-h6">AI Analysis</h2>
              <v-spacer />
              <v-btn
                color="primary"
                :loading="analysisBusy"
                prepend-icon="mdi-robot"
                size="small"
                @click="onAnalyze"
              >
                {{ analysis?.status === 'success' ? 'Regenerate' : 'Generate' }}
              </v-btn>
            </div>
            <div class="text-caption text-medium-emphasis mb-3" v-if="usage">
              {{ usage.requests }} / {{ usage.daily_budget }} used today
              <span v-if="usage.headroom === 0" class="text-warning ml-2">(budget exhausted)</span>
            </div>

            <v-progress-linear
              v-if="analysis?.status === 'pending' || analysis?.status === 'generating'"
              indeterminate
              color="info"
              class="mb-2"
            />
            <v-skeleton-loader
              v-if="analysis?.status === 'pending' || analysis?.status === 'generating'"
              type="paragraph"
            />

            <v-alert v-if="analysisErr" type="error" :text="analysisErr" class="mb-2" />

            <div v-if="analysis?.status === 'success' && analysis.content">
              <p style="white-space: pre-wrap" class="text-body-1 mb-2">{{ analysis.content }}</p>
              <div class="text-caption text-medium-emphasis d-flex align-center">
                <v-chip size="x-small" :color="statusColor(analysis.status)" class="mr-2">{{ analysis.status }}</v-chip>
                <span class="mr-2">Powered by <strong>{{ providerLabel(analysis.provider) }}</strong> ({{ analysis.model_id }})</span>
                <span>· {{ analysis.tokens_in + analysis.tokens_out }} tokens</span>
              </div>
            </div>

            <v-alert v-else-if="analysis?.status === 'failed'" type="error" :text="analysis.error || 'Analysis failed'" />

            <div v-else-if="!analysis" class="text-body-2 text-medium-emphasis">
              Click <strong>Generate</strong> to get an AI-powered 2-3 paragraph analysis of this trade
              using your recent trading context.
            </div>
          </v-card>

          <ImageGrid :trade-id="trade.id" class="mb-4" />
          <TagPicker :trade-id="trade.id" class="mb-4" />
          <JournalEditor :trade-id="trade.id" />
        </v-col>
      </v-row>
    </div>
  </v-container>
</template>