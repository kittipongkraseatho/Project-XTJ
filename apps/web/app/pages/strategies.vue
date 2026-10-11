<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useStrategies, type Strategy, type StrategyVersion } from '~/composables/useStrategies'
import type { StrategyRuleSet } from '~/composables/useStrategies-types'
import { defaultStrategyRules } from '~/composables/strategy-utils'

definePageMeta({ name: 'strategies' })

const { list, create, listVersions, createVersion } = useStrategies()
const strategies = ref<Strategy[]>([])
const selectedId = ref('')
const versions = ref<StrategyVersion[]>([])
const loading = ref(true)
const errorMsg = ref<string | null>(null)
const creating = ref(false)
const creatingVersion = ref(false)
const showCreate = ref(false)
const showVersion = ref(false)
const name = ref('')
const description = ref('')
const rulesText = ref(JSON.stringify(defaultStrategyRules(), null, 2))

const selected = computed(() => strategies.value.find((item) => item.id === selectedId.value) ?? null)

async function refresh() {
  loading.value = true
  try {
    strategies.value = await list()
    if (!selectedId.value) selectedId.value = strategies.value[0]?.id ?? ''
    if (selectedId.value) versions.value = await listVersions(selectedId.value)
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err?.message ?? 'Failed to load strategies.'
  } finally {
    loading.value = false
  }
}

async function selectStrategy(id: string) {
  selectedId.value = id
  try { versions.value = await listVersions(id) } catch (err: any) { errorMsg.value = err?.message ?? 'Failed to load versions.' }
}

async function saveStrategy() {
  if (!name.value.trim()) { errorMsg.value = 'Name is required.'; return }
  creating.value = true
  try {
    const item = await create({ name: name.value.trim(), description: description.value.trim() })
    strategies.value.unshift(item)
    selectedId.value = item.id
    versions.value = []
    showCreate.value = false
    name.value = ''; description.value = ''
    errorMsg.value = null
  } catch (err: any) { errorMsg.value = err?.message ?? 'Failed to create strategy.' } finally { creating.value = false }
}

async function saveVersion() {
  if (!selectedId.value) return
  let rules: StrategyRuleSet
  try { rules = JSON.parse(rulesText.value) as StrategyRuleSet } catch { errorMsg.value = 'Rules must be valid JSON.'; return }
  if (rules.schema_version !== 1 || !rules.entry?.length || !rules.exit?.length || !(rules.risk?.max_volume > 0)) { errorMsg.value = 'Rules need schema_version 1, entry, exit, and positive risk values.'; return }
  creatingVersion.value = true
  try {
    const version = await createVersion(selectedId.value, rules)
    versions.value.unshift(version)
    showVersion.value = false
    errorMsg.value = null
  } catch (err: any) { errorMsg.value = err?.message ?? 'Failed to create version.' } finally { creatingVersion.value = false }
}

onMounted(refresh)
</script>

<template>
  <v-container class="strategies-page py-8">
    <div class="page-kicker">R2 / STRATEGY WORKBENCH</div>
    <div class="hero-row mb-7"><div><h1 class="text-h3 font-weight-bold mb-2">Rules that stay explainable.</h1><p class="text-body-1 text-medium-emphasis">Describe a strategy, then append immutable versions as your thinking evolves.</p></div><v-btn color="primary" size="large" @click="showCreate = true">New strategy</v-btn></div>
    <v-alert v-if="errorMsg" type="warning" variant="tonal" class="mb-5" closable @click:close="errorMsg = null">{{ errorMsg }}</v-alert>
    <div v-if="loading" class="state-panel">Loading strategies…</div>
    <div v-else-if="strategies.length === 0" class="state-panel empty-state"><div class="empty-mark">◇</div><h2 class="text-h5 mb-2">No strategies yet</h2><p class="text-medium-emphasis mb-5">Start with a name. Add the first rule version when you are ready.</p><v-btn color="primary" @click="showCreate = true">Create strategy</v-btn></div>
    <v-row v-else>
      <v-col cols="12" md="4">
        <v-card class="panel-card" variant="outlined"><v-card-title class="d-flex justify-space-between align-center">Strategies <span class="text-caption text-medium-emphasis">{{ strategies.length }}</span></v-card-title><v-list bg-color="transparent" lines="two"><v-list-item v-for="item in strategies" :key="item.id" :active="item.id === selectedId" rounded="0" @click="selectStrategy(item.id)"><template #prepend><span class="strategy-glyph">{{ item.name.slice(0, 1).toUpperCase() }}</span></template><v-list-item-title>{{ item.name }}</v-list-item-title><v-list-item-subtitle>{{ item.status }} · {{ item.current_version_id ? 'versioned' : 'draft' }}</v-list-item-subtitle></v-list-item></v-list></v-card>
      </v-col>
      <v-col cols="12" md="8">
        <v-card v-if="selected" class="panel-card" variant="outlined"><v-card-text class="pa-6"><div class="d-flex justify-space-between align-start ga-4"><div><div class="text-overline text-medium-emphasis">Strategy</div><h2 class="text-h4 mb-2">{{ selected.name }}</h2><p class="text-medium-emphasis">{{ selected.description || 'No description yet.' }}</p></div><v-chip color="primary" variant="tonal">{{ selected.status }}</v-chip></div><v-divider class="my-6" /><div class="d-flex justify-space-between align-center mb-3"><div><div class="text-overline text-medium-emphasis">Immutable history</div><div class="text-h6">{{ versions.length }} version{{ versions.length === 1 ? '' : 's' }}</div></div><v-btn variant="tonal" color="primary" @click="rulesText = JSON.stringify(defaultStrategyRules(), null, 2); showVersion = true">Append version</v-btn></div><div v-if="versions.length === 0" class="text-medium-emphasis py-8">No version yet. The strategy is only a name until a rule set is saved.</div><div v-for="version in versions" :key="version.id" class="version-row"><div class="version-index">v{{ version.version_no }}</div><div class="flex-grow-1"><div class="font-weight-medium">Schema {{ version.schema_version }} · created {{ new Date(version.created_at).toLocaleString() }}</div><div class="text-caption text-medium-emphasis">Setup {{ version.rules.setup.symbols.join(', ') || 'any symbol' }} · max volume {{ version.rules.risk.max_volume }}</div></div><v-chip size="small" variant="outlined">read only</v-chip></div></v-card-text></v-card>
      </v-col>
    </v-row>
    <v-dialog v-model="showCreate" max-width="520"><v-card><v-card-title>New strategy</v-card-title><v-card-text><v-text-field v-model="name" label="Name" autofocus /><v-textarea v-model="description" label="Description" rows="3" /></v-card-text><v-card-actions><v-spacer /><v-btn variant="text" @click="showCreate = false">Cancel</v-btn><v-btn color="primary" :loading="creating" @click="saveStrategy">Create strategy</v-btn></v-card-actions></v-card></v-dialog>
    <v-dialog v-model="showVersion" max-width="760"><v-card><v-card-title>Append immutable version</v-card-title><v-card-text><p class="text-body-2 text-medium-emphasis mb-3">This rule set becomes a permanent version. Future edits create another version.</p><v-textarea v-model="rulesText" label="Rule set (JSON)" rows="18" class="rules-editor" /></v-card-text><v-card-actions><v-spacer /><v-btn variant="text" @click="showVersion = false">Cancel</v-btn><v-btn color="primary" :loading="creatingVersion" @click="saveVersion">Save version</v-btn></v-card-actions></v-card></v-dialog>
  </v-container>
</template>

<style scoped>
.strategies-page { max-width: 1220px; }.page-kicker { color: #a78bfa; font-size: .72rem; font-weight: 800; letter-spacing: .16em; }.hero-row { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; }.panel-card { background: rgba(18,24,38,.74) !important; border-color: rgba(148,163,184,.14) !important; }.strategy-glyph { display: grid; place-items: center; width: 34px; height: 34px; margin-right: 12px; border: 1px solid rgba(167,139,250,.5); color: #c4b5fd; font-weight: 800; }.version-row { display: flex; align-items: center; gap: 14px; padding: 16px 0; border-bottom: 1px solid rgba(148,163,184,.1); }.version-row:last-child { border-bottom: 0; }.version-index { color: #c4b5fd; font-weight: 800; width: 35px; }.rules-editor :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: .83rem; }.state-panel { padding: 72px 24px; text-align: center; color: #94a3b8; }.empty-mark { color: #a78bfa; font-size: 3rem; margin-bottom: 12px; }
@media (max-width: 720px) { .hero-row { align-items: flex-start; flex-direction: column; } }
</style>
