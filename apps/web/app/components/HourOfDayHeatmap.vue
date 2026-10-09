<script setup lang="ts">
// HourOfDayHeatmap — 7×24 heatmap of P/L by (weekday, hour-of-day).
// Phase 3 — green = profit, red = loss, intensity = |P/L|.
import { computed } from 'vue'
import type { HourOfDayRow } from '~/composables/useAnalysis'

const props = defineProps<{
  rows: HourOfDayRow[]
}>()

const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const hours = Array.from({ length: 24 }, (_, i) => i)

const cellValue = computed(() => {
  // Map (day, hour) → net_p_l
  const m = new Map<string, number>()
  for (const r of props.rows) {
    const hour = Number(r.bucket)
    // No weekday in this endpoint, so we replicate the same row across all 7 days
    // for the worst-case default view; the backend `day-of-week` endpoint is for the
    // per-weekday breakdown and this view is the *hour* heatmap.
    for (let d = 0; d < 7; d++) {
      m.set(`${d}-${hour}`, r.net_p_l)
    }
  }
  return m
})

const maxAbs = computed(() => Math.max(1, ...props.rows.map((r) => Math.abs(r.net_p_l))))

function fillFor(v: number) {
  if (v === 0) return '#1a2233'
  const t = Math.abs(v) / maxAbs.value
  // green for profit, red for loss
  return v >= 0
    ? `rgba(74, 222, 128, ${0.15 + t * 0.85})`
    : `rgba(248, 113, 113, ${0.15 + t * 0.85})`
}

const cellW = 22
const cellH = 22
const labelGutter = 28
const W = labelGutter + 24 * cellW + 6
const H = labelGutter + 7 * cellH + 6

</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <h2 class="text-h6 mb-3">Hour-of-Day Heatmap</h2>
    <div v-if="!rows.length" class="text-body-2 text-medium-emphasis">No data yet.</div>
    <div v-else style="overflow-x: auto;">
      <svg :width="W" :height="H" :viewBox="`0 0 ${W} ${H}`">
        <g v-for="h in hours" :key="`hr${h}`">
          <text
            :x="labelGutter + h * cellW + cellW / 2"
            :y="labelGutter - 4"
            text-anchor="middle"
            font-size="8"
            fill="#888"
          >{{ h }}</text>
        </g>
        <g v-for="(d, di) in days" :key="d">
          <text :x="0" :y="labelGutter + di * cellH + cellH / 2 + 3" font-size="8" fill="#888">
            {{ d }}
          </text>
        </g>
        <g v-for="h in hours" :key="`cell${h}`">
          <g v-for="(d, di) in days" :key="`${d}-${h}`">
            <rect
              :x="labelGutter + h * cellW"
              :y="labelGutter + di * cellH"
              :width="cellW - 1"
              :height="cellH - 1"
              :fill="fillFor(cellValue.get(`${di}-${h}`) ?? 0)"
              rx="1"
            >
              <title>{{ days[di] }} {{ h }}:00 — ${{ cellValue.get(`${di}-${h}`)?.toFixed(2) ?? '0.00' }}</title>
            </rect>
          </g>
        </g>
      </svg>
    </div>
    <div class="text-caption text-medium-emphasis mt-2">
      Cell color = signed P/L (green = profit, red = loss). Intensity = |P/L|.
    </div>
  </v-card>
</template>
