<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { TradingAccount } from '@trading-journal/shared'
import { useImports } from '~/composables/useImports'
import { useBehavior, type BehaviorProfile } from '~/composables/useBehavior'
import { percent } from '~/composables/behavior-utils'

definePageMeta({ name: 'behavior' })

const { listAccounts } = useImports()
const { getProfile, calculateProfile } = useBehavior()
const accounts = ref<TradingAccount[]>([])
const accountId = ref('')
const profile = ref<BehaviorProfile | null>(null)
const loading = ref(true)
const calculating = ref(false)
const errorMsg = ref<string | null>(null)

function dateValue(date: Date) {
  return date.toISOString().slice(0, 10)
}

const toDate = ref(dateValue(new Date()))
const fromDate = ref(dateValue(new Date(Date.now() - 30 * 24 * 60 * 60 * 1000)))

const metricCards = computed(() => {
  if (!profile.value) return []
  const m = profile.value.metrics
  return [
    { label: 'Win rate', value: percent(m.win_rate), tone: 'blue' },
    { label: 'Expectancy', value: m.expectancy.toFixed(2), tone: m.expectancy >= 0 ? 'green' : 'red' },
    { label: 'Profit factor', value: m.profit_factor ? m.profit_factor.toFixed(2) : '—', tone: 'violet' },
    { label: 'Trades / day', value: m.trades_per_day.toFixed(1), tone: 'amber' },
  ]
})

const concentration = computed(() => {
  const shares = profile.value?.metrics.symbol_share ?? {}
  return Object.entries(shares).sort(([, a], [, b]) => b - a).slice(0, 6)
})

async function loadProfile() {
  if (!accountId.value) return
  loading.value = true
  try {
    profile.value = await getProfile(accountId.value)
    errorMsg.value = null
  } catch (err: any) {
    profile.value = null
    errorMsg.value = err?.message ?? 'No behavior profile is available yet.'
  } finally {
    loading.value = false
  }
}

async function calculate() {
  if (!accountId.value) return
  calculating.value = true
  try {
    const from = `${fromDate.value}T00:00:00Z`
    const to = `${toDate.value}T00:00:00Z`
    profile.value = await calculateProfile(accountId.value, from, to)
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err?.message ?? 'Behavior calculation failed.'
  } finally {
    calculating.value = false
  }
}

onMounted(async () => {
  try {
    accounts.value = (await listAccounts()).accounts
    accountId.value = accounts.value[0]?.id ?? ''
    if (accountId.value) await loadProfile()
  } catch (err: any) {
    errorMsg.value = err?.message ?? 'Failed to load accounts.'
    loading.value = false
  }
})
</script>

<template>
  <v-container class="behavior-page py-8">
    <div class="page-kicker">R2 / BEHAVIOR INTELLIGENCE</div>
    <div class="hero-row mb-7">
      <div>
        <h1 class="text-h3 font-weight-bold mb-2">Read the pattern, not the noise.</h1>
        <p class="text-body-1 text-medium-emphasis">Deterministic signals from your completed trades. No black box, no order authority.</p>
      </div>
      <div class="sample-stamp">R2–V1<br><span>UTC WINDOW</span></div>
    </div>

    <v-alert v-if="errorMsg" type="warning" variant="tonal" class="mb-5" closable @click:close="errorMsg = null">
      {{ errorMsg }}
    </v-alert>

    <v-card class="control-card mb-5" variant="outlined">
      <v-card-text class="d-flex flex-wrap ga-3 align-end">
        <v-select v-model="accountId" :items="accounts" item-title="display_name" item-value="id" label="Trading account" density="comfortable" hide-details class="control-account" @update:model-value="loadProfile" />
        <v-text-field v-model="fromDate" type="date" label="From (UTC)" density="comfortable" hide-details class="control-date" />
        <v-text-field v-model="toDate" type="date" label="To (UTC, exclusive)" density="comfortable" hide-details class="control-date" />
        <v-btn color="primary" size="large" :loading="calculating" :disabled="!accountId" @click="calculate">Calculate profile</v-btn>
      </v-card-text>
    </v-card>

    <div v-if="loading" class="state-panel">Loading profile…</div>
    <div v-else-if="!profile" class="state-panel empty-state">
      <div class="empty-mark">◎</div>
      <h2 class="text-h5 mb-2">Start with a completed-trade window</h2>
      <p class="text-medium-emphasis">Choose an account and calculate its first profile.</p>
    </div>
    <template v-else>
      <div class="profile-strip mb-5">
        <div>
          <div class="text-overline text-medium-emphasis">Profile status</div>
          <div class="d-flex align-center ga-3">
            <span class="status-dot" :class="profile.status" />
            <strong class="text-h5">{{ profile.status === 'ready' ? 'Ready to inspect' : 'More data needed' }}</strong>
          </div>
        </div>
        <div class="profile-meta"><span>{{ profile.sample_size }} completed trades</span><span>{{ new Date(profile.window_start).toLocaleDateString() }} → {{ new Date(profile.window_end).toLocaleDateString() }}</span></div>
      </div>

      <div class="metric-grid mb-5">
        <v-card v-for="metric in metricCards" :key="metric.label" class="metric-card" variant="outlined">
          <v-card-text><div class="metric-line" :class="`tone-${metric.tone}`" /><div class="text-caption text-medium-emphasis">{{ metric.label }}</div><div class="metric-value">{{ metric.value }}</div></v-card-text>
        </v-card>
      </div>

      <v-row>
        <v-col cols="12" md="7">
          <v-card class="panel-card" variant="outlined">
            <v-card-title>Findings</v-card-title>
            <v-card-text>
              <div v-if="profile.findings.length === 0" class="text-medium-emphasis py-5">No threshold crossed in this window.</div>
              <div v-for="finding in profile.findings" :key="finding.code" class="finding-row">
                <div class="finding-icon" :class="finding.severity">{{ finding.severity === 'critical' ? '!' : '·' }}</div>
                <div class="flex-grow-1"><div class="font-weight-medium">{{ finding.code.replaceAll('_', ' ') }}</div><div class="text-caption text-medium-emphasis">{{ finding.count }} evidence point{{ finding.count === 1 ? '' : 's' }} · IDs available for review</div></div>
                <v-chip size="small" variant="tonal" :color="finding.severity === 'critical' ? 'error' : finding.severity === 'warning' ? 'warning' : 'info'">{{ finding.severity }}</v-chip>
              </div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="5">
          <v-card class="panel-card" variant="outlined">
            <v-card-title>Symbol concentration</v-card-title>
            <v-card-text>
              <div v-if="concentration.length === 0" class="text-medium-emphasis py-5">No completed trades in this profile.</div>
              <div v-for="[symbol, share] in concentration" :key="symbol" class="share-row"><div class="d-flex justify-space-between text-caption mb-1"><span>{{ symbol }}</span><span>{{ percent(share) }}</span></div><v-progress-linear :model-value="share * 100" color="primary" rounded height="6" /></div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>
    </template>
  </v-container>
</template>

<style scoped>
.behavior-page { max-width: 1220px; }
.page-kicker { color: #60a5fa; font-size: .72rem; font-weight: 800; letter-spacing: .16em; }
.hero-row, .profile-strip { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; }
.sample-stamp { border: 1px solid rgba(96,165,250,.5); color: #93c5fd; font-size: .8rem; letter-spacing: .12em; line-height: 1.5; padding: 10px 13px; text-align: right; }
.sample-stamp span { color: rgba(148,163,184,.8); font-size: .58rem; }
.control-card, .panel-card, .metric-card { background: rgba(18,24,38,.74) !important; border-color: rgba(148,163,184,.14) !important; }
.control-account { min-width: 240px; flex: 1; }.control-date { min-width: 170px; }
.profile-strip { border-left: 3px solid #3b82f6; padding: 14px 18px; background: linear-gradient(90deg, rgba(59,130,246,.12), transparent); }
.profile-meta { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; color: #94a3b8; font-size: .82rem; }
.status-dot { width: 10px; height: 10px; border-radius: 99px; background: #f59e0b; box-shadow: 0 0 14px currentColor; }.status-dot.ready { background: #34d399; }.status-dot.insufficient_data { background: #f59e0b; }
.metric-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }.metric-line { height: 3px; width: 34px; margin-bottom: 18px; }.tone-blue { background: #60a5fa; }.tone-green { background: #34d399; }.tone-red { background: #fb7185; }.tone-violet { background: #a78bfa; }.tone-amber { background: #fbbf24; }.metric-value { font-size: 1.65rem; font-weight: 750; margin-top: 5px; }
.finding-row { display: flex; align-items: center; gap: 12px; padding: 13px 0; border-bottom: 1px solid rgba(148,163,184,.1); }.finding-row:last-child { border-bottom: 0; }.finding-icon { width: 26px; height: 26px; display: grid; place-items: center; border-radius: 50%; color: #0b0f19; background: #93c5fd; font-weight: 900; }.finding-icon.warning { background: #fbbf24; }.finding-icon.critical { background: #fb7185; }
.share-row { margin: 17px 0; }.state-panel { padding: 72px 24px; text-align: center; color: #94a3b8; }.empty-mark { color: #60a5fa; font-size: 3rem; margin-bottom: 12px; }
@media (max-width: 720px) { .hero-row, .profile-strip { align-items: flex-start; flex-direction: column; }.profile-meta { align-items: flex-start; }.metric-grid { grid-template-columns: repeat(2, 1fr); } }
</style>
