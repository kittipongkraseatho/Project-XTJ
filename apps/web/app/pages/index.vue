<script setup lang="ts">
// Landing page — marketing + CTA. Public route.

definePageMeta({ name: 'index' })
</script>

<template>
  <v-container class="py-16">
    <v-row justify="center" align="center" class="text-center">
      <v-col cols="12" md="8" lg="6">
        <h1 class="text-h3 text-md-h2 font-weight-bold mb-4">
          Your <span class="brand-gradient">Trading Journal</span>, organised.
        </h1>
        <p class="text-body-1 text-medium-emphasis mb-8">
          Upload MT5 history or auto-sync via MetaApi. Track trades, analytics, risk, journal notes — all in one place.
        </p>
        <div class="d-flex justify-center ga-3">
          <v-btn color="primary" size="large" to="/register">Get started — Free</v-btn>
          <v-btn variant="outlined" size="large" to="/login">Sign in</v-btn>
        </div>
      </v-col>
    </v-row>

    <v-row class="mt-16" justify="center">
      <v-col v-for="feature in [
        { icon: 'mdi-chart-line', title: 'Analytics', desc: 'Net P/L, Win Rate, Profit Factor, Drawdown, Equity Curve.' },
        { icon: 'mdi-book-open-page-variant', title: 'Journal', desc: 'Strategy, entry/exit reasons, emotion, mistakes. Tagged + searchable.' },
        { icon: 'mdi-cloud-sync', title: 'Auto Sync', desc: 'Connect MT5 via MetaApi — historical + incremental.' },
        { icon: 'mdi-shield-lock', title: 'Multi-tenant', desc: 'Your data is yours. Tenant isolation + audit logs by default.' },
      ]" :key="feature.title" cols="12" sm="6" md="3">
        <v-card variant="tonal" class="pa-4 h-100">
          <v-icon size="32" color="primary" class="mb-2">{{ feature.icon }}</v-icon>
          <div class="text-h6 mb-1">{{ feature.title }}</div>
          <div class="text-body-2 text-medium-emphasis">{{ feature.desc }}</div>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>