// nuxt.config.ts — Trading Journal web (Nuxt 3 + Vuetify + Pinia + PWA)

export default defineNuxtConfig({
  compatibilityDate: '2025-10-01',
  devtools: { enabled: true },

  modules: [
    '@pinia/nuxt',
    '@vueuse/nuxt',
    '@vite-pwa/nuxt',
  ],

  typescript: {
    strict: true,
    typeCheck: false, // runs via `pnpm typecheck`
  },

  app: {
    head: {
      title: 'Trading Journal',
      htmlAttrs: { lang: 'en' },
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'theme-color', content: '#3B82F6' },
        { name: 'description', content: 'Multi-tenant trading journal SaaS' },
      ],
      link: [
        { rel: 'icon', type: 'image/svg+xml', href: '/icons/favicon.svg' },
        { rel: 'apple-touch-icon', href: '/icons/icon-192.png' },
      ],
    },
  },

  runtimeConfig: {
    public: {
      apiBaseUrl: process.env.NUXT_PUBLIC_API_BASE_URL || 'http://localhost:8080',
    },
  },

  css: ['vuetify/styles', '@mdi/font/css/materialdesignicons.css', '~/assets/scss/global.scss'],

  build: {
    transpile: ['vuetify'],
  },

  vite: {
    ssr: {
      noExternal: ['vuetify'],
    },
    define: { 'process.env.DEBUG': 'false' },
  },

  imports: {
    dirs: ['stores'],
  },

  // ── PWA configuration (spec §4.2 / §7A) ──────────────────────────────
  pwa: {
    registerType: 'autoUpdate',
    manifest: {
      name: 'Trading Journal',
      short_name: 'TJ',
      description: 'Multi-tenant trading journal SaaS',
      theme_color: '#3B82F6',
      background_color: '#0B0F19',
      display: 'standalone',
      orientation: 'portrait',
      start_url: '/',
      scope: '/',
      icons: [
        { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
        { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
        { src: '/icons/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
      ],
    },
    workbox: {
      navigateFallback: '/',
      cleanupOutdatedCaches: true,
      runtimeCaching: [
        {
          urlPattern: ({ request }) =>
            ['document', 'script', 'style', 'image', 'font'].includes(request.destination),
          handler: 'StaleWhileRevalidate',
          options: { cacheName: 'tj-shell-v1' },
        },
        {
          urlPattern: /\/v1\//,
          handler: 'NetworkFirst',
          options: {
            cacheName: 'tj-api-v1',
            networkTimeoutSeconds: 5,
            expiration: { maxEntries: 50, maxAgeSeconds: 300 },
          },
        },
      ],
    },
    client: { installPrompt: true, periodicSyncForUpdates: 3600 },
    devOptions: { enabled: false, type: 'module' },
  },

  nitro: {
    compressPublicAssets: true,
  },
})