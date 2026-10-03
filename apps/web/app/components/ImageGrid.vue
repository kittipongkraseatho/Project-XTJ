<script setup lang="ts">
// ImageGrid — upload + display Cloudinary trade screenshots.
import { ref, onMounted } from 'vue'
import { useImages } from '~/composables/useImages'

const props = defineProps<{ tradeId: string }>()

const { listForTrade, attach } = useImages()

interface TradeImage {
  id: string
  public_id: string
  secure_url: string
  thumbnail_url: string
  format: string
  width: number
  height: number
  bytes: number
  created_at: string
}

const images = ref<TradeImage[]>([])
const uploading = ref(false)
const loading = ref(true)
const errorMsg = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    images.value = await listForTrade(props.tradeId)
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function onPick(e: Event) {
  const t = e.target as HTMLInputElement
  const file = t.files?.[0]
  if (!file) return
  uploading.value = true
  errorMsg.value = null
  try {
    const img = await attach(props.tradeId, file)
    images.value = [...images.value, img]
  } catch (err: any) {
    errorMsg.value = `${err?.code ?? 'ERROR'}: ${err?.message ?? 'upload failed'}`
  } finally {
    uploading.value = false
    t.value = ''
  }
}

function formatBytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}
</script>

<template>
  <v-card variant="outlined" class="pa-4">
    <div class="d-flex align-center mb-3">
      <h2 class="text-h6">Screenshots</h2>
      <v-spacer />
      <v-btn variant="tonal" size="small" prepend-icon="mdi-upload" :loading="uploading" @click="($refs.fileInput as HTMLInputElement)?.click()">
        Upload
      </v-btn>
      <input
        ref="fileInput"
        type="file"
        accept="image/png,image/jpeg,image/webp"
        style="display:none"
        @change="onPick"
      />
    </div>

    <v-alert v-if="errorMsg" type="error" variant="tonal" density="compact" :text="errorMsg" class="mb-3" />

    <v-progress-circular v-if="loading" indeterminate color="primary" size="24" />

    <div v-else-if="images.length === 0" class="text-caption text-medium-emphasis text-center py-4">
      No screenshots yet. Upload a chart/entry/exit snapshot.
    </div>

    <div v-else class="d-flex flex-wrap ga-2">
      <v-card
        v-for="img in images"
        :key="img.id"
        variant="outlined"
        class="pa-1"
        :width="120"
      >
        <v-img :src="img.thumbnail_url || img.secure_url" :alt="img.public_id" cover height="100" />
        <div class="text-caption pa-1 d-flex justify-space-between align-center">
          <span>{{ formatBytes(img.bytes) }}</span>
          <v-btn icon="mdi-open-in-new" variant="text" size="x-small" :href="img.secure_url" target="_blank" />
        </div>
      </v-card>
    </div>
  </v-card>
</template>