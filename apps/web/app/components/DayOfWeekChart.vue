<script setup lang="ts">
// DayOfWeekChart — 7-bar P/L by weekday (0=Sunday, 6=Saturday).
// Phase 3 — pure SVG, consistent with EquityCurveChart.
import { computed } from 'vue'
import type { DayOfWeekRow } from '~/composables/useAnalysis'

const props = defineProps<{
  rows: DayOfWeekRow[]
}>()

const labels = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

const merged = computed(() => {
  const out: { label: string; net_p_l: number; trade_count: number; win_rate: number }[] = []
  for (let i = 0; i < 7; i++) {
    const r = props.rows.find((x) => x.bucket === String(i))
    out.push({
      label: labels[i],
      net_p_l: r?.net_p_l ?? 0,
      trade_count: r?.trade_count ?? 0,
      win_rate: r?.win_rate ?? 0,
    })
  }
  return out
})

const W = 360
const H = 160
const PAD_X = 24
const PAD_Y = 16
const innerW = W - PAD_X * 2
const innerH = H - PAD_Y * 2

const maxAbs = computed(() => Math.max(1, ...merged.value.map((r) => Math.abs(r.net_p_l))))
const zeroY = computed(() => {
  const range = maxAbs.value * 2
  return PAD_Y + (maxAbs.value / range) * innerH
})

function barX(i: number) {
  const bw = innerW / 7
  return PAD_X + i * bw + bw * 0.15
}
function barW() {
  const bw = innerW / 7
  return bw * 0.7
}
function barH(v: number) {
  return (Math.abs(v) / maxAbs.value) * (innerH / 2)
}
function barY(v: number) {
  if (v >= 0) return zeroY.value - barH(v)
  return zeroY.value
}
function fill(v: number) {
  return v >= 0 ? '#4ade80' : '#f87171'
}
function fmtMoney(v: number) {
  return v.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <h2 class="text-h6 mb-3">Day-of-Week P/L</h2>
    <div v-if="!rows.length" class="text-body-2 text-medium-emphasis">No data yet.</div>
    <svg v-else :width="W" :height="H" :viewBox="`0 0 ${W} ${H}`" style="width: 100%; max-width: 100%;">
      <line :x1="PAD_X" :y1="zeroY" :x2="W - PAD_X" :y2="zeroY" stroke="#444" stroke-width="0.5" />
      <g v-for="(r, i) in merged" :key="i">
        <rect
          :x="barX(i)"
          :y="barY(r.net_p_l)"
          :width="barW()"
          :height="barH(r.net_p_l)"
          :fill="fill(r.net_p_l)"
          opacity="0.85"
        >
          <title>{{ r.label }}: {{ fmtMoney(r.net_p_l) }} ({{ r.trade_count }} trades)</title>
        </rect>
        <text
          :x="barX(i) + barW() / 2"
          :y="H - 4"
          text-anchor="middle"
          font-size="9"
          fill="#aaa"
        >{{ r.label }}</text>
      </g>
    </svg>
  </v-card>
</template>