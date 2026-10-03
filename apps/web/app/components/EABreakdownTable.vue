<script setup lang="ts">
// EABreakdownTable — MT5 magic (EA identifier) breakdown table.
// Phase 3 — magic=0 maps to "Manual trades"; others render as "EA #N".
import type { EARow } from '~/composables/useAnalysis'

defineProps<{
  rows: EARow[]
}>()

function fmtMoney(v: number) {
  return v.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}
function fmtPct(v: number) {
  return (v * 100).toFixed(1) + '%'
}
function magicLabel(magic: string): string {
  if (magic === '0') return 'Manual trades'
  return `EA #${magic}`
}
function rowColor(v: number) {
  return v >= 0 ? 'text-success' : 'text-error'
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <h2 class="text-h6 mb-3">EA Breakdown</h2>
    <div v-if="!rows.length" class="text-body-2 text-medium-emphasis">No EA-tagged trades in this range.</div>
    <v-table v-else density="comfortable">
      <thead>
        <tr>
          <th>EA</th>
          <th class="text-right">Trades</th>
          <th class="text-right">Win %</th>
          <th class="text-right">Net P/L</th>
          <th class="text-right">Profit Factor</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in rows" :key="r.bucket">
          <td class="font-weight-medium">{{ magicLabel(r.bucket) }}</td>
          <td class="text-right">{{ r.trade_count }}</td>
          <td class="text-right">{{ fmtPct(r.win_rate) }}</td>
          <td class="text-right font-weight-medium" :class="rowColor(r.net_p_l)">
            {{ fmtMoney(r.net_p_l) }}
          </td>
          <td class="text-right">{{ r.profit_factor.toFixed(2) }}</td>
        </tr>
      </tbody>
    </v-table>
  </v-card>
</template>