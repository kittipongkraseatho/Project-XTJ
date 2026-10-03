<script setup lang="ts">
// Dashboard. Auth-only.
import { useAuthStore } from '~/stores/auth'

definePageMeta({ name: 'dashboard', middleware: ['auth.global'] })

const auth = useAuthStore()
const router = useRouter()

async function onLogout() {
  await auth.logout()
  await router.push('/login')
}
</script>

<template>
  <v-container class="py-12">
    <v-row>
      <v-col cols="12">
        <h1 class="text-h4 mb-2">Welcome, {{ auth.user?.display_name ?? 'trader' }} 👋</h1>
        <p class="text-body-2 text-medium-emphasis">
          Plan: <strong>{{ auth.plan?.name ?? '—' }}</strong>
          <span v-if="auth.plan"> · {{ auth.plan.currency }} {{ (auth.plan.price_cents / 100).toFixed(0) }}/{{ auth.plan.interval }}</span>
        </p>
      </v-col>
    </v-row>

    <v-row class="mt-6">
      <v-col cols="12" sm="6" md="3" v-for="card in [
        { icon: 'mdi-chart-line', label: 'Net P/L', value: '—', color: 'primary' },
        { icon: 'mdi-target', label: 'Win Rate', value: '—', color: 'success' },
        { icon: 'mdi-trending-up', label: 'Profit Factor', value: '—', color: 'accent' },
        { icon: 'mdi-arrow-down-bold', label: 'Max Drawdown', value: '—', color: 'warning' },
      ]" :key="card.label">
        <v-card variant="tonal" class="pa-4">
          <div class="d-flex align-center mb-1">
            <v-icon :color="card.color" class="mr-2">{{ card.icon }}</v-icon>
            <span class="text-body-2 text-medium-emphasis">{{ card.label }}</span>
          </div>
          <div class="text-h5 font-weight-bold">{{ card.value }}</div>
        </v-card>
      </v-col>
    </v-row>

    <v-row class="mt-6">
      <v-col cols="12">
        <v-card variant="outlined" class="pa-6 text-center">
          <v-icon size="40" color="medium-emphasis" class="mb-2">mdi-rocket-launch-outline</v-icon>
          <div class="text-h6 mb-2">Phase 0 complete</div>
          <div class="text-body-2 text-medium-emphasis mb-4">
            Auth, tenant, RBAC, OAuth (Google + Line), PWA, observability — ready.
            <br />
            Trade upload + analytics land in <strong>Phase 1</strong>.
          </div>
          <v-btn variant="outlined" color="error" @click="onLogout">Logout</v-btn>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>