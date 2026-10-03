<script setup lang="ts">
// TagPicker — multi-select tags with create-new.
// Loads all tenant tags, lets user pick or create, persists to PUT /v1/trades/:id/tags.
import { ref, onMounted, watch, computed } from 'vue'
import { useTags } from '~/composables/useTags'

const props = defineProps<{ tradeId: string }>()
const emit = defineEmits<{ (e: 'saved'): void }>()

const { list, create, listForTrade, setForTrade } = useTags()

interface Tag {
  id: string
  name: string
  color: string | null
}

const allTags = ref<Tag[]>([])
const selectedIds = ref<string[]>([])
const newTagName = ref('')
const saving = ref(false)
const loading = ref(true)
const showCreate = ref(false)

const selectedTags = computed(() => allTags.value.filter((t) => selectedIds.value.includes(t.id)))

async function load() {
  loading.value = true
  try {
    const [tags, ids] = await Promise.all([list(), listForTrade(props.tradeId)])
    allTags.value = tags
    selectedIds.value = ids
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(() => props.tradeId, load)

async function persist() {
  saving.value = true
  try {
    await setForTrade(props.tradeId, selectedIds.value)
    emit('saved')
  } finally {
    saving.value = false
  }
}

async function onCreate() {
  if (!newTagName.value.trim()) return
  const created = await create(newTagName.value.trim())
  allTags.value = [...allTags.value, created].sort((a, b) => a.name.localeCompare(b.name))
  selectedIds.value = [...selectedIds.value, created.id]
  newTagName.value = ''
  showCreate.value = false
  await persist()
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <div class="d-flex align-center mb-3">
      <h2 class="text-h6">Tags</h2>
      <v-spacer />
      <v-btn variant="text" size="small" prepend-icon="mdi-plus" @click="showCreate = !showCreate">New</v-btn>
    </div>

    <v-progress-circular v-if="loading" indeterminate color="primary" size="24" />

    <template v-else>
      <v-expand-transition>
        <div v-if="showCreate" class="mb-3">
          <v-text-field
            v-model="newTagName"
            label="New tag name"
            density="compact"
            hide-details
            @keyup.enter="onCreate"
          >
            <template #append>
              <v-btn size="small" color="primary" :disabled="!newTagName.trim()" @click="onCreate">Add</v-btn>
            </template>
          </v-text-field>
        </div>
      </v-expand-transition>

      <v-combobox
        v-model="selectedIds"
        :items="allTags"
        item-title="name"
        item-value="id"
        label="Tags for this trade"
        multiple
        chips
        closable-chips
        density="compact"
        @update:model-value="persist"
      />

      <div v-if="selectedTags.length" class="mt-2">
        <v-chip
          v-for="t in selectedTags"
          :key="t.id"
          :color="t.color ?? 'primary'"
          size="small"
          class="mr-1 mb-1"
        >
          {{ t.name }}
        </v-chip>
      </div>
      <div v-else class="text-caption text-medium-emphasis mt-2">No tags assigned to this trade.</div>
    </template>
  </v-card>
</template>