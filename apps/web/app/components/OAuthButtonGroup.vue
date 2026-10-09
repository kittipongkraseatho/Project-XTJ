<script setup lang="ts">
// OAuthButtonGroup — renders Continue-with-Google + Continue-with-Line buttons.
// Phase 0 providers (per spec §7A.1): Google + Line.
// BE only returns enabled providers in /v1/me/configureroutes-by-providers —
// we infer from /healthz reachability here; simpler: just try both buttons.

import { ref, onMounted } from 'vue'

defineProps<{ mode?: 'login' | 'register' }>()

const providers = [
  { id: 'google', label: 'Google', icon: 'mdi-google', color: 'primary' },
  { id: 'line',   label: 'LINE',   icon: 'mdi-message-text-outline', color: 'success' },
] as const

const enabled = ref<Record<string, boolean>>({ google: true, line: true })

onMounted(() => {
  // Phase 0 doesn't expose a "list providers" endpoint; assume both enabled if env configured.
  // Future: GET /v1/auth/oauth/providers to populate `enabled`.
})

function start(provider: string) {
  window.location.href = `/v1/auth/oauth/${provider}/start`
}
</script>

<template>
  <div class="d-flex flex-column ga-2">
    <v-btn
      v-for="p in providers" :key="p.id"
      :disabled="!enabled[p.id]"
      variant="outlined"
      :prepend-icon="p.icon"
      block
      @click="start(p.id)"
    >
      Continue with {{ p.label }}
    </v-btn>
  </div>
</template>
