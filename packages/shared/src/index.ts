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

// ── Phase 1 types ──────────────────────────────────────────────────────

export type TradeDirection = 'long' | 'short'
export type TradeOutcome = 'win' | 'loss' | 'breakeven'

export interface TradeDTO {
  id: string
  symbol: string
  direction: TradeDirection
  open_time: string
  close_time: string
  net_p_l: number
  outcome: TradeOutcome
}

export interface Trade {
  id: string
  tenant_id: string
  user_id: string
  trading_account_id: string
  position_id: string | null
  symbol: string
  direction: TradeDirection
  volume: number
  open_price: number
  close_price: number | null
  open_time: string
  close_time: string | null
  holding_seconds: number | null
  gross_profit: number
  gross_loss: number
  net_p_l: number
  commission: number
  swap: number
  outcome: TradeOutcome
  is_partial: boolean
  created_at: string
}

export interface TradingAccount {
  id: string
  user_id: string
  tenant_id: string
  broker: 'mt5' | 'mt4' | 'ctrader' | 'custom'
  broker_account_id: string
  display_name: string
  currency: string
  leverage: number | null
  kind: 'demo' | 'real'
  is_active: boolean
  created_at: string
}

export type ImportStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface ImportJob {
  id: string
  tenant_id: string
  imported_file_id: string
  status: ImportStatus
  started_at: string | null
  finished_at: string | null
  parsed_rows: number
  inserted_rows: number
  skipped_rows: number
  errors: unknown[]
  created_at: string
}