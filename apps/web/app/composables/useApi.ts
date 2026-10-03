// useApi — typed HTTP client with auto-refresh on 401.
// Spec §4.1.

import type { AuthTokens } from '@trading-journal/shared'
import { useAuthStore } from '~/stores/auth'

interface ApiError {
  code: string
  message: string
  details?: Record<string, any>
  status: number
}

const refreshOnceQueue: { promise: Promise<void> | null } = { promise: null }

async function refreshTokens(): Promise<void> {
  if (refreshOnceQueue.promise) return refreshOnceQueue.promise
  const auth = useAuthStore()
  if (!auth.refreshToken) throw new Error('no refresh token')
  const cfg = useRuntimeConfig()
  refreshOnceQueue.promise = (async () => {
    try {
      const tokens = await $fetch<AuthTokens>(`${cfg.public.apiBaseUrl}/v1/auth/refresh`, {
        method: 'POST',
        body: { refresh_token: auth.refreshToken },
      })
      auth.setTokens(tokens)
    } finally {
      refreshOnceQueue.promise = null
    }
  })()
  return refreshOnceQueue.promise
}

export async function apiGet<T = any>(path: string): Promise<T> {
  return apiRequest<T>('GET', path)
}

export async function apiPost<T = any>(path: string, body?: any): Promise<T> {
  return apiRequest<T>('POST', path, body)
}

async function apiRequest<T>(method: string, path: string, body?: any, _isRetry = false): Promise<T> {
  const cfg = useRuntimeConfig()
  const auth = useAuthStore()
  const headers: Record<string, string> = {}
  if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`

  try {
    return await $fetch<T>(`${cfg.public.apiBaseUrl}${path}`, {
      method: method as any,
      body,
      headers,
    })
  } catch (err: any) {
    const status = err?.response?.status ?? 0
    const data = err?.response?._data
    // Auto-refresh on 401 (once)
    if (status === 401 && !_isRetry && auth.refreshToken && path !== '/v1/auth/refresh') {
      try {
        await refreshTokens()
        return apiRequest<T>(method, path, body, true)
      } catch { /* fall through */ }
    }
    throw {
      code: data?.error?.code ?? 'NETWORK_ERROR',
      message: data?.error?.message ?? err?.message ?? 'unknown error',
      details: data?.error?.details,
      status,
    } as ApiError
  }
}

export async function authLogout(): Promise<void> {
  await apiPost('/v1/auth/logout')
}

export type { ApiError }