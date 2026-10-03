<script setup lang="ts">
// Receives access + refresh tokens via URL after OAuth callback, stores via Pinia, then redirects to /dashboard.
// Spec §7A.7 / Step 13.

import { useAuthStore } from '~/stores/auth'
import { apiGet } from '~/composables/useApi'

definePageMeta({ name: 'auth-oauth-complete' })

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const status = ref<'processing' | 'success' | 'error'>('processing')
const errorMessage = ref<string | null>(null)

onMounted(async () => {
  const access = route.query.access as string | undefined
  const refresh = route.query.refresh as string | undefined
  const expiresIn = Number(route.query.expires_in ?? 900)
  if (!access || !refresh) {
    status.value = 'error'
    errorMessage.value = 'Missing tokens in callback URL.'
    return
  }
  try {
    auth.setTokens({ access_token: access, refresh_token: refresh, token_type: 'Bearer', expires_in: expiresIn })
    await auth.fetchMe()
    // Clean URL (avoid tokens lingering in browser history)
    router.replace('/dashboard')
    status.value = 'success'
  } catch (err: any) {
    status.value = 'error'
    errorMessage.value = err?.message ?? 'Failed to load profile after sign-in.'
  }
})
</script>

<template>
  <v-container class="py-16 text-center">
    <v-progress-circular v-if="status === 'processing'" indeterminate color="primary" size="48" />
    <v-icon v-else-if="status === 'success'" color="success" size="48">mdi-check-circle</v-icon>
    <v-icon v-else color="error" size="48">mdi-alert-circle</v-icon>
    <p class="mt-4 text-body-1">
      <span v-if="status === 'processing'">Completing sign-in…</span>
      <span v-else-if="status === 'success'">Success! Redirecting…</span>
      <span v-else>{{ errorMessage }}</span>
    </p>
  </v-container>
</template>