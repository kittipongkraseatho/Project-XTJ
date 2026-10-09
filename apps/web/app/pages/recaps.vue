<script setup lang="ts">
// /recaps — Phase 9 weekly recap page.
//
// Top: account dropdown + week picker (defaults to last completed Monday UTC).
// "Generate weekly recap" button (disabled if budget exhausted).
// Below: list of past recaps (expandable → content).

import { ref, computed, onMounted } from 'vue'
import { useRecap, type Recap } from '~/composables/useRecap'
import { useImports } from '~/composables/useImports'
import { useAnalysis, type Usage } from '~/composables/useAnalysis'
import type { TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'recaps' })

const { listAccounts } = useImports()
const { generate, list, pollUntilDone } = useRecap()
const { getUsage } = useAnalysis()

const accounts = ref<TradingAccount[]>([])
const selectedAccountId = ref<string>('')
const recaps = ref<Recap[]>([])
const loading = ref(false)
const errorMsg = ref<string | null>(null)
const generating = ref(false)
const generatingStatus = ref<string | null>(null)
const expandedRecapId = ref<string | null>(null)
const usage = ref<Usage | null>(null)

function defaultWeekStart(): string {
  // Last completed Monday UTC in YYYY-MM-DD.
  const d = new Date()
  const day = d.getUTCDay() // 0..6 (Sun..Sat)
  const offsetToMonday = day === 0 ? 6 : day - 1
  const lastMonday = new Date(d)
  lastMonday.setUTCDate(d.getUTCDate() - offsetToMonday - 7)
  return lastMonday.toISOString().slice(0, 10)
}

const weekStart = ref<string>(defaultWeekStart())

async function refresh() {
  loading.value = true
  try {
    if (accounts.value.length === 0) {
      const a = await listAccounts()
      accounts.value = a.accounts
      if (accounts.value.length > 0 && !selectedAccountId.value) {
        selectedAccountId.value = accounts.value[0].id
      }
    }
    if (selectedAccountId.value) {
      recaps.value = await list(selectedAccountId.value)
    }
    usage.value = await getUsage()
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load recaps'
  } finally {
    loading.value = false
  }
}

async function onGenerate() {
  if (!selectedAccountId.value) return
  generating.value = true
  generatingStatus.value = 'pending'
  try {
    const r = await generate(selectedAccountId.value, weekStart.value)
    generatingStatus.value = r.status
    if (r.status === 'success') {
      // Cached result.
      await refresh()
      return
    }
    const done = await pollUntilDone(r.id, { intervalMs: 2500, maxMs: 90_000 })
    if (done) {
      generatingStatus.value = done.status
      if (done.status === 'failed' && done.error) {
        errorMsg.value = done.error
      }
    } else {
      generatingStatus.value = 'timeout'
    }
    await refresh()
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to generate recap'
  } finally {
    generating.value = false
  }
}

function toggleExpand(id: string) {
  expandedRecapId.value = expandedRecapId.value === id ? null : id
}

const budgetText = computed(() => {
  if (!usage.value) return ''
  return `${usage.value.requests + (usage.value.recap_requests ?? 0)} / ${usage.value.daily_budget} used today`
})

onMounted(refresh)
</script>

<template>
  <v-container>
    <v-row>
      <v-col cols="12">
        <h1 class="text-h4 mb-2">Weekly Recap</h1>
        <p class="text-medium-emphasis mb-6">AI-generated weekly summary for a single trading account.</p>
      </v-col>
    </v-row>

    <v-row v-if="errorMsg">
      <v-col cols="12">
        <v-alert type="error" closable @click:close="errorMsg = null">{{ errorMsg }}</v-alert>
      </v-col>
    </v-row>

    <v-card class="mb-4">
      <v-card-text>
        <v-row align="end">
          <v-col cols="12" md="5">
            <v-select
              v-model="selectedAccountId"
              :items="accounts"
              item-title="display_name"
              item-value="id"
              placeholder="Select account"
              label="Account"
              density="compact"
              @update:model-value="refresh"
            />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field
              v-model="weekStart"
              label="Week start (Monday UTC)"
              type="date"
              density="compact"
              hint="YYYY-MM-DD"
            />
          </v-col>
          <v-col cols="12" md="3" class="d-flex">
            <v-btn
              color="primary"
              :loading="generating"
              :disabled="!selectedAccountId || generating"
              block
              @click="onGenerate"
            >
              Generate
            </v-btn>
          </v-col>
        </v-row>
        <div v-if="generatingStatus" class="text-caption text-medium-emphasis mt-2">
          Status: <strong>{{ generatingStatus }}</strong>
        </div>
        <div v-if="usage" class="text-caption text-medium-emphasis mt-1">
          Budget: {{ budgetText }} · headroom {{ usage.headroom }}
        </div>
      </v-card-text>
    </v-card>

    <v-card>
      <v-card-title>Past recaps</v-card-title>
      <v-card-text>
        <div v-if="loading" class="text-center pa-4">Loading…</div>
        <div v-else-if="recaps.length === 0" class="text-center text-medium-emphasis pa-4">
          No recaps yet. Pick a week and click Generate.
        </div>
        <v-list v-else>
          <v-list-item
            v-for="r in recaps"
            :key="r.id"
            @click="toggleExpand(r.id)"
          >
            <template #prepend>
              <v-chip
                :color="r.status === 'success' ? 'success' : r.status === 'failed' ? 'error' : 'warning'"
                size="small"
                class="mr-2"
              >
                {{ r.status }}
              </v-chip>
            </template>
            <v-list-item-title>
              Week of {{ r.week_start }} → {{ r.week_end }}
            </v-list-item-title>
            <v-list-item-subtitle>
              {{ r.provider }} / {{ r.model_id }} · {{ r.tokens_in }}↑ {{ r.tokens_out }}↓
            </v-list-item-subtitle>
            <template v-if="expandedRecapId === r.id" #append>
              <v-icon>mdi-chevron-up</v-icon>
            </template>
            <template v-else #append>
              <v-icon>mdi-chevron-down</v-icon>
            </template>
            <v-expand-transition>
              <div v-if="expandedRecapId === r.id" class="pa-4 text-body-1" style="white-space: pre-wrap">
                <template v-if="r.status === 'success' && r.content">{{ r.content }}</template>
                <template v-else-if="r.status === 'failed'">
                  <v-alert type="error" density="compact">{{ r.error || 'Generation failed' }}</v-alert>
                </template>
                <template v-else>
                  <v-progress-circular indeterminate size="20" width="2" /> Generating…
                </template>
              </div>
            </v-expand-transition>
          </v-list-item>
        </v-list>
      </v-card-text>
    </v-card>
  </v-container>
</template>
