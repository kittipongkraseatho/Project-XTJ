// @trading-journal/shared — cross-stack DTOs, enums, error codes.
// Single source of truth for the FE↔BE contract.

export type Role = 'owner' | 'admin' | 'member' | 'viewer'

export type PlanCode = 'free' | 'upload_journal' | 'starter' | 'pro'

export interface AuthUser {
  id: string
  email: string
  display_name: string
}

export interface AuthTenant {
  id: string
  name: string
  slug: string
}

export interface AuthPlan {
  id: string
  code: PlanCode
  name: string
  currency: string
  price_cents: number
  interval: 'monthly' | 'yearly'
}

export interface AuthTokens {
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number // seconds
}

// Stable error codes returned by BE (spec §7.5). FE uses these to render UX.
export const ERROR_CODES = [
  'VALIDATION_FAILED',
  'UNAUTHORIZED',
  'INVALID_CREDENTIALS',
  'PASSWORD_TOO_WEAK',
  'EMAIL_ALREADY_EXISTS',
  'USER_NOT_FOUND',
  'TENANT_NOT_FOUND',
  'MEMBERSHIP_NOT_FOUND',
  'INSUFFICIENT_ROLE',
  'REFRESH_TOKEN_INVALID',
  'REFRESH_TOKEN_EXPIRED',
  'REFRESH_TOKEN_REUSE_DETECTED',
  'OAUTH_STATE_INVALID',
  'OAUTH_PROVIDER_NOT_CONFIGURED',
  'OAUTH_CODE_EXCHANGE_FAILED',
  'OAUTH_USERINFO_FAILED',
  'RATE_LIMITED',
  'INTERNAL_ERROR',
] as const

export type ErrorCode = (typeof ERROR_CODES)[number]

export interface ApiErrorBody {
  code: ErrorCode | 'NETWORK_ERROR'
  message: string
  details?: Record<string, unknown>
}

export interface ApiErrorResponse {
  error: ApiErrorBody
}