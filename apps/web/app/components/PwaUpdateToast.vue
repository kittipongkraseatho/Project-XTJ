<script setup lang="ts">
// PWA update toast — autoUpdate SW triggers an offline-ready / need-refresh event.
import { ref, onMounted } from 'vue'
import { useRegisterSW } from 'virtual:pwa-register/vue'

const showReload = ref(false)
const offlineReady = ref(false)

const intervalFn = () => {
  intervalMs.value = 60 * 60 * 1000 // hourly
}

onMounted(() => {
  if (typeof window === 'undefined') return
  try {
    const sw = useRegisterSW({
      onNeedRefresh() { showReload.value = true },
      onOfflineReady() { offlineReady.value = true },
      onRegisterError(err) { console.warn('SW register error', err) },
    })
    intervalFn()
    void sw
  } catch {
    // PWA module not registered (dev / unsupported). Silently ignore.
  }
})

const intervalMs = ref<number>(0)

async function reload() {
  if (typeof window === 'undefined') return
  try {
    const reg = await navigator.serviceWorker.getRegistration()
    await reg?.waiting?.postMessage({ type: 'SKIP_WAITING' })
  } catch {}
  window.location.reload()
}
</script>

<template>
  <v-snackbar v-model="showReload" location="bottom" :timeout="-1" color="surface-variant">
      New version available
      <template #actions>
        <v-btn variant="text" @click="reload">Reload</v-btn>
      </template>
    </v-snackbar>
  <v-snackbar v-model="offlineReady" location="bottom" :timeout="3000" color="success">
    App is ready to work offline
  </v-snackbar>
</template>
