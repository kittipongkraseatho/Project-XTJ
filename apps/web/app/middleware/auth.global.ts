// Global auth middleware — runs on every navigation.
// Redirects unauth users from /dashboard to /login.
// Spec §4.1.

import { useAuthStore } from '~/stores/auth'

export default defineNuxtRouteMiddleware(async (to) => {
  // Hydrate once per session
  const auth = useAuthStore()
  if (!auth.user) {
    await auth.hydrate()
  }

  // Guard /dashboard (and any future authed pages)
  const requiresAuth = to.path.startsWith('/dashboard') || to.path.startsWith('/trades') ||
    to.path.startsWith('/analytics') || to.path.startsWith('/settings') ||
    to.path.startsWith('/journal')

  if (requiresAuth && !auth.isAuthed) {
    return navigateTo(`/login?redirect=${encodeURIComponent(to.fullPath)}`)
  }

  // If already authed and visiting /login or /register, redirect to /dashboard
  if ((to.path === '/login' || to.path === '/register') && auth.isAuthed) {
    return navigateTo('/dashboard')
  }
})