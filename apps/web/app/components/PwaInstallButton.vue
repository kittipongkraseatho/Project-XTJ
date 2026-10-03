<script setup lang="ts">
// PWA install button — appears when the browser fires beforeinstallprompt.
import { ref, onMounted, onBeforeUnmount } from 'vue'

const canInstall = ref(false)
let deferred: any = null

function onPrompt(e: Event) {
  e.preventDefault()
  deferred = e
  canInstall.value = true
}

async function install() {
  if (!deferred) return
  deferred.prompt()
  const choice = await deferred.userChoice
  if (choice.outcome === 'accepted') canInstall.value = false
  deferred = null
}

onMounted(() => window.addEventListener('beforeinstallprompt', onPrompt))
onBeforeUnmount(() => window.removeEventListener('beforeinstallprompt', onPrompt))
</script>

<template>
  <v-btn v-if="canInstall" variant="text" size="small" prepend-icon="mdi-download" @click="install">
    Install
  </v-btn>
</template>