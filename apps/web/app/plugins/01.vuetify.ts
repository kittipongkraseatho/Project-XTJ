// Vuetify plugin with dark trader-palette theme.
// Palette per spec §4.1: bg #0B0F19, surface #121826, primary #3B82F6.

import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

export default defineNuxtPlugin((app) => {
  const vuetify = createVuetify({
    ssr: true,
    components,
    directives,
    theme: {
      defaultTheme: 'tjDark',
      themes: {
        tjDark: {
          dark: true,
          colors: {
            background: '#0B0F19',
            surface: '#121826',
            'surface-variant': '#1B2236',
            primary: '#3B82F6',
            secondary: '#60A5FA',
            accent: '#22D3EE',
            success: '#22C55E',
            warning: '#F59E0B',
            error: '#EF4444',
            'on-background': '#E5E7EB',
            'on-surface': '#F3F4F6',
            'on-primary': '#FFFFFF',
          },
        },
      },
    },
    defaults: {
      VBtn: { variant: 'flat' },
      VTextField: { variant: 'outlined', density: 'comfortable' },
      VCard: { rounded: 'lg' },
    },
  })
  app.vueApp.use(vuetify)
})