// useAuth — thin wrapper around the auth store for ergonomics in components.

import { useAuthStore } from '~/stores/auth'
import type { AuthTokens, AuthUser, AuthTenant, AuthPlan } from '@trading-journal/shared'
import { apiPost } from '~/composables/useApi'

interface RegisterInput { email: string; password: string; display_name: string }
interface LoginInput { email: string; password: string }

export function useAuth() {
  const auth = useAuthStore()
  const router = useRouter()

  async function register(input: RegisterInput) {
    const res = await apiPost<{ user: AuthUser; tenant: AuthTenant; plan: AuthPlan; tokens: AuthTokens }>(
      '/v1/auth/register', input,
    )
    auth.setTokens(res.tokens)
    auth.setMe({ user: res.user, tenant: res.tenant, plan: res.plan, role: 'owner' })
    return res
  }

  async function login(input: LoginInput) {
    const res = await apiPost<{ user: AuthUser; tenant: AuthTenant; plan: AuthPlan; tokens: AuthTokens }>(
      '/v1/auth/login', input,
    )
    auth.setTokens(res.tokens)
    auth.setMe({ user: res.user, tenant: res.tenant, plan: res.plan, role: 'owner' })
    return res
  }

  async function logout() {
    await auth.logout()
    await router.push('/login')
  }

  return { auth, register, login, logout }
}