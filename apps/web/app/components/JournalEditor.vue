<script setup lang="ts">
// JournalEditor — VeeValidate-style form for journal fields per spec §32.
import { ref, watch, onMounted } from 'vue'
import { useJournal } from '~/composables/useJournal'

const props = defineProps<{ tradeId: string }>()
const emit = defineEmits<{ (e: 'saved'): void }>()

const { get, upsert } = useJournal()

interface JournalEntry {
  id: string
  trade_id: string
  strategy: string | null
  entry_reason: string | null
  exit_reason: string | null
  emotion: 'calm' | 'neutral' | 'fomo' | 'anxious' | 'confident' | 'fearful' | 'greedy' | null
  mistake: string | null
  notes: string | null
  updated_at: string
}

const emotions = [
  { value: 'calm', label: 'Calm' },
  { value: 'neutral', label: 'Neutral' },
  { value: 'fomo', label: 'FOMO' },
  { value: 'anxious', label: 'Anxious' },
  { value: 'confident', label: 'Confident' },
  { value: 'fearful', label: 'Fearful' },
  { value: 'greedy', label: 'Greedy' },
]

const strategy = ref('')
const entryReason = ref('')
const exitReason = ref('')
const emotion = ref<string | null>(null)
const mistake = ref('')
const notes = ref('')
const saving = ref(false)
const saveMsg = ref<string | null>(null)
const lastSaved = ref<string | null>(null)
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    const entry: JournalEntry | null = await get(props.tradeId)
    if (entry) {
      strategy.value = entry.strategy ?? ''
      entryReason.value = entry.entry_reason ?? ''
      exitReason.value = entry.exit_reason ?? ''
      emotion.value = entry.emotion ?? null
      mistake.value = entry.mistake ?? ''
      notes.value = entry.notes ?? ''
      lastSaved.value = entry.updated_at
    }
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => props.tradeId, load)

async function save() {
  saving.value = true
  saveMsg.value = null
  try {
    await upsert(props.tradeId, {
      strategy: strategy.value || null,
      entry_reason: entryReason.value || null,
      exit_reason: exitReason.value || null,
      emotion: emotion.value as any,
      mistake: mistake.value || null,
      notes: notes.value || null,
    })
    saveMsg.value = 'Saved'
    lastSaved.value = new Date().toISOString()
    emit('saved')
  } catch (err: any) {
    saveMsg.value = `Save failed: ${err?.message ?? err}`
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <h2 class="text-h6 mb-3">Journal</h2>
    <v-progress-circular v-if="loading" indeterminate color="primary" size="24" />

    <template v-else>
      <v-text-field v-model="strategy" label="Strategy" placeholder="Breakout / Pullback / Trend…" density="compact" />
      <v-text-field v-model="entryReason" label="Entry reason" placeholder="Why I entered this trade…" density="compact" class="mt-2" />
      <v-text-field v-model="exitReason" label="Exit reason" placeholder="TP / SL / manual…" density="compact" class="mt-2" />
      <v-select v-model="emotion" :items="emotions" item-title="label" item-value="value" label="Emotion" clearable density="compact" class="mt-2" />
      <v-text-field v-model="mistake" label="Mistake (if any)" density="compact" class="mt-2" />
      <v-textarea v-model="notes" label="Notes" rows="3" density="compact" class="mt-2" />

      <div class="d-flex align-center ga-3 mt-4">
        <v-btn color="primary" :loading="saving" @click="save">Save</v-btn>
        <span v-if="saveMsg" class="text-caption" :class="saveMsg === 'Saved' ? 'text-success' : 'text-error'">
          {{ saveMsg }}
        </span>
        <v-spacer />
        <span v-if="lastSaved" class="text-caption text-medium-emphasis">
          Last saved {{ new Date(lastSaved).toLocaleString() }}
        </span>
      </div>
    </template>
  </v-card>
</template>