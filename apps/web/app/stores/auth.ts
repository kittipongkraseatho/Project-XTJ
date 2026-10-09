// Pinia auth store — single source of truth for user / accessToken / currentTenant.
// Spec §4.1.

import { defineStore } from 'pinia'
import type { AuthUser, AuthTenant, AuthPlan, AuthTokens } from '@trading-journal/shared'
import { apiGet, authLogout } from '~/composables/useApi'

interface AuthState {
  user: AuthUser | null
  tenant: AuthTenant | null
  plan: AuthPlan | null
  role: string | null
  accessToken: string | null
  refreshToken: string | null
  expiresAt: number | null // epoch ms; null = no token
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    user: null,
    tenant: null,
    plan: null,
    role: null,
    accessToken: null,
    refreshToken: null,
    expiresAt: null,
  }),

  actions: {
    async hydrate() {
      // Restore tokens from localStorage (best-effort).
      if (typeof window === 'undefined') return
      const at = window.localStorage.getItem('tj_access')
      const rt = window.localStorage.getItem('tj_refresh')
      const exp = window.localStorage.getItem('tj_exp')
      if (at && rt && exp) {
        this.accessToken = at
        this.refreshToken = rt
        this.expiresAt = Number(exp)
        try { await this.fetchMe() } catch { await this.clearTokens() }
      }
    },

    setTokens(t: AuthTokens) {
      this.accessToken = t.access_token
      this.refreshToken = t.refresh_token
      this.expiresAt = Date.now() + t.expires_in * 1000
      if (typeof window !== 'undefined') {
        window.localStorage.setItem('tj_access', t.access_token)
        window.localStorage.setItem('tj_refresh', t.refresh_token)
        window.localStorage.setItem('tj_exp', String(this.expiresAt))
      }
    },

    setMe(payload: { user: AuthUser; tenant: AuthTenant; plan: AuthPlan; role: string }) {
      this.user = payload.user
      this.tenant = payload.tenant
      this.plan = payload.plan
      this.role = payload.role
    },

    async clearTokens() {
      this.accessToken = null
      this.refreshToken = null
      this.expiresAt = null
      this.user = null
      this.tenant = null
      this.plan = null
      this.role = null
      if (typeof window !== 'undefined') {
        window.localStorage.removeItem('tj_access')
        window.localStorage.removeItem('tj_refresh')
        window.localStorage.removeItem('tj_exp')
      }
    },

    async logout() {
      try { await authLogout() } catch { /* best effort */ }
      await this.clearTokens()
    },

    async fetchMe() {
      const me = await apiGet('/v1/me')
      this.setMe(me)
    },

    isExpired(): boolean {
      if (!this.expiresAt) return true
      // Refresh 30s before actual expiry
      return Date.now() > this.expiresAt - 30_000
    },
  },

  getters: {
    isAuthed: (s): boolean => !!s.accessToken && !!s.user,
  },
})
