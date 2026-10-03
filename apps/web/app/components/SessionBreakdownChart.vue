<script setup lang="ts">
// SessionBreakdownChart — FX trading session bar chart.
// Phase 3 — 4 bars (asia / london / newyork / sydney) with overlapping buckets.
// Sessions are independent — a London/NY overlap trade counts in both.
import { computed } from 'vue'
import type { SessionRow } from '~/composables/useAnalysis'

const props = defineProps<{
  rows: SessionRow[]
}>()

interface Labeled extends SessionRow {
  label: string
}

// Make the FE-facing label stable regardless of bucket ordering.
const labels: Record<string, string> = {
  asia: 'Asia (00–09 UTC)',
  london: 'London (07–16 UTC)',
  newyork: 'New York (13–22 UTC)',
  sydney: 'Sydney (22–07 UTC)',
}

const labeled = computed<Labeled[]>(() =>
  props.rows
    .map((r) => ({ ...r, label: labels[r.bucket] ?? r.bucket }))
    .sort((a, b) => b.net_p_l - a.net_p_l),
)

const maxAbs = computed(() =>
  Math.max(1, ...labeled.value.map((r) => Math.abs(r.net_p_l))),
)

function fmtMoney(v: number) {
  return v.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}
function barWidth(v: number) {
  return Math.abs(v) / maxAbs.value * 100
}
function barColor(v: number) {
  return v >= 0 ? 'var(--v-theme-success)' : 'var(--v-theme-error)'
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <h2 class="text-h6 mb-3">Session Breakdown</h2>
    <div v-if="!rows.length" class="text-body-2 text-medium-emphasis">No trades in this range.</div>
    <div v-else>
      <div v-for="r in labeled" :key="r.bucket" class="mb-3">
        <div class="d-flex justify-space-between text-caption mb-1">
          <span class="font-weight-medium">{{ r.label }}</span>
          <span :class="r.net_p_l >= 0 ? 'text-success' : 'text-error'">
            {{ fmtMoney(r.net_p_l) }} · {{ r.trade_count }} trades
          </span>
        </div>
        <div class="d-flex" style="position: relative; height: 16px;">
          <div
            :style="{
              width: `${barWidth(r.net_p_l)}%`,
              background: barColor(r.net_p_l),
              height: '100%',
              borderRadius: '3px',
            }"
          />
        </div>
      </div>
    </div>
  </v-card>
</template>