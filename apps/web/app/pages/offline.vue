<script setup lang="ts">
// PWA offline fallback page (spec §7A.7).
definePageMeta({ name: 'offline' })
</script>

<template>
  <v-container class="py-16 text-center">
    <v-icon size="64" color="medium-emphasis" class="mb-4">mdi-wifi-off</v-icon>
    <h1 class="text-h4 mb-2">You're offline</h1>
    <p class="text-body-1 text-medium-emphasis">
      Some features aren't available while offline. Cached pages will still work.
    </p>
    <v-btn class="mt-4" color="primary" @click="$router.push('/')">Back to home</v-btn>
  </v-container>
</template>