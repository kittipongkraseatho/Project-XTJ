<script setup lang="ts">
// Default app shell — dark theme, app header (with PWA install button), footer.

import { useAuthStore } from '~/stores/auth'
import { NAV_ITEMS } from '~/navigation'

const auth = useAuthStore()
const route = useRoute()

const navItems = computed(() => {
  if (!auth.isAuthed) return []
  return NAV_ITEMS
})

async function onLogout() {
  await auth.logout()
  await navigateTo('/login')
}
</script>

<template>
  <v-app-bar color="surface" elevation="0" density="comfortable" border>
    <v-app-bar-title>
      <NuxtLink to="/" class="text-decoration-none">
        <span class="text-h6 brand-gradient font-weight-bold">Trading Journal</span>
      </NuxtLink>
    </v-app-bar-title>
    <v-spacer />
    <template v-if="auth.user">
      <template v-for="item in navItems" :key="item.to">
        <v-btn
          v-if="item.enabled"
          :to="item.to"
          variant="text"
          size="small"
        >
          {{ item.title }}
        </v-btn>
        <v-tooltip v-else location="bottom">
          <template #activator="{ props }">
            <span
              v-bind="props"
              class="d-inline-flex"
              tabindex="0"
              role="button"
              aria-disabled="true"
              :aria-label="`${item.title} — Coming soon`"
            >
              <v-btn variant="text" size="small" disabled>
                {{ item.title }}
              </v-btn>
            </span>
          </template>
          <span>Coming soon</span>
        </v-tooltip>
      </template>
    </template>
    <PwaInstallButton />
    <template v-if="auth.user">
      <v-menu>
        <template #activator="{ props }">
          <v-btn icon v-bind="props" aria-label="Account menu">
            <v-icon>mdi-account-circle</v-icon>
          </v-btn>
        </template>
        <v-list>
          <v-list-item :to="'/dashboard'" :title="auth.user.display_name" :subtitle="auth.user.email" />
          <v-list-item @click="onLogout" prepend-icon="mdi-logout" title="Logout" />
        </v-list>
      </v-menu>
    </template>
    <template v-else-if="route.name !== 'login' && route.name !== 'register'">
      <v-btn :to="'/login'" variant="text">Login</v-btn>
      <v-btn :to="'/register'" color="primary" class="ml-2">Sign up</v-btn>
    </template>
  </v-app-bar>

  <v-main>
    <slot />
  </v-main>

  <v-footer color="surface" class="text-caption text-medium-emphasis py-3" border>
    <span>© 2026 Trading Journal</span>
    <v-spacer />
    <span class="text-disabled">Phase 10</span>
  </v-footer>

  <PwaUpdateToast />
</template>
