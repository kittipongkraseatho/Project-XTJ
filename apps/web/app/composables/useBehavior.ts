import { apiGet, apiPost } from '~/composables/useApi'

export interface BehaviorMetrics {
  win_rate: number
  expectancy: number
  average_win: number
  average_loss: number
  profit_factor: number
  net_pl: number
  average_hold_seconds: number
  trades_per_day: number
  symbol_share: Record<string, number>
  direction_share: Record<string, number>
  session_share: Record<string, number>
  magic_share: Record<string, number>
}

export interface BehaviorFinding {
  code: string
  severity: 'info' | 'warning' | 'critical'
  count: number
  evidence_trade_ids: string[]
  evidence_timestamps: string[]
}

export interface BehaviorProfile {
  id: string
  trading_account_id: string
  window_start: string
  window_end: string
  algorithm_version: string
  status: 'insufficient_data' | 'ready'
  sample_size: number
  metrics: BehaviorMetrics
  findings: BehaviorFinding[]
  created_at: string
}

export function useBehavior() {
  function getProfile(accountId: string) {
    return apiGet<BehaviorProfile>(`/v1/accounts/${accountId}/behavior-profile`)
  }

  function calculateProfile(accountId: string, from: string, to: string) {
    return apiPost<BehaviorProfile>(`/v1/accounts/${accountId}/behavior-profile`, { from, to })
  }

  return { getProfile, calculateProfile }
}
