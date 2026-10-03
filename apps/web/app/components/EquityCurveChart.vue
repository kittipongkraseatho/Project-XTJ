<script setup lang="ts">
// EquityCurveChart — pure SVG line chart for trade equity over time.
// Avoids heavyweight chart libs (Chart.js) — keeps bundle small for PWA.

interface Point {
  closed_at: string
  net_p_l: number
  equity: number
}

const props = withDefaults(defineProps<{
  points: Point[]
  height?: number
}>(), { height: 220 })

const viewBox = computed(() => {
  const w = 800
  const h = props.height
  return `0 0 ${w} ${h}`
})

const stats = computed(() => {
  const pts = props.points
  if (!pts.length) return { min: 0, max: 0, peak: 0, trough: 0, final: 0 }
  let min = pts[0].equity, peak = pts[0].equity, trough = pts[0].equity
  for (const p of pts) {
    if (p.equity < min) min = p.equity
    if (p.equity > peak) peak = p.equity
    if (p.equity < trough) trough = p.equity
  }
  return { min, max: peak, peak, trough, final: pts[pts.length - 1].equity }
})

const path = computed(() => {
  const pts = props.points
  if (pts.length < 2) return ''
  const w = 800
  const h = props.height
  const padding = 24
  const innerW = w - padding * 2
  const innerH = h - padding * 2
  const xStep = innerW / (pts.length - 1)
  const range = stats.value.max - stats.value.min || 1
  let d = ''
  pts.forEach((p, i) => {
    const x = padding + i * xStep
    const y = padding + innerH - ((p.equity - stats.value.min) / range) * innerH
    d += (i === 0 ? 'M' : 'L') + `${x.toFixed(1)},${y.toFixed(1)} `
  })
  return d.trim()
})

const fillPath = computed(() => {
  const pts = props.points
  if (pts.length < 2) return ''
  const w = 800
  const h = props.height
  const padding = 24
  const innerW = w - padding * 2
  const innerH = h - padding * 2
  const xStep = innerW / (pts.length - 1)
  const range = stats.value.max - stats.value.min || 1
  let d = `M${padding},${padding + innerH} `
  pts.forEach((p, i) => {
    const x = padding + i * xStep
    const y = padding + innerH - ((p.equity - stats.value.min) / range) * innerH
    d += `L${x.toFixed(1)},${y.toFixed(1)} `
  })
  d += `L${(padding + innerW).toFixed(1)},${(padding + innerH).toFixed(1)} Z`
  return d
})

const lineColor = computed(() => stats.value.final >= 0 ? 'success' : 'error')

function fmtMoney(n: number) {
  return n.toLocaleString('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <div class="d-flex align-center justify-space-between mb-3">
      <h2 class="text-h6">Equity curve</h2>
      <div v-if="points.length" class="d-flex ga-3 text-caption">
        <div>
          <span class="text-medium-emphasis">Peak</span>
          <span class="ml-1 font-weight-medium text-success">{{ fmtMoney(stats.peak) }}</span>
        </div>
        <div>
          <span class="text-medium-emphasis">Trough</span>
          <span class="ml-1 font-weight-medium text-error">{{ fmtMoney(stats.trough) }}</span>
        </div>
        <div>
          <span class="text-medium-emphasis">Final</span>
          <span class="ml-1 font-weight-medium" :class="stats.final >= 0 ? 'text-success' : 'text-error'">
            {{ fmtMoney(stats.final) }}
          </span>
        </div>
      </div>
    </div>

    <div v-if="!points.length" class="text-caption text-medium-emphasis text-center py-8">
      No trades yet — equity curve appears after your first import.
      </div>

    <svg v-else :viewBox="viewBox" preserveAspectRatio="none" style="width:100%; height:220px">
      <path :d="fillPath" :fill="lineColor === 'success' ? '#22c55e22' : '#ef444422'" />
      <path :d="path" :stroke="lineColor === 'success' ? '#22c55e' : '#ef4444'"
            stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round" />
      <circle :cx="0" :cy="0" r="0" /> <!-- keep parser happy -->
    </svg>
  </v-card>
</template>