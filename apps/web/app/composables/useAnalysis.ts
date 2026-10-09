// useAnalysis — Phase 6 AI analysis composables.
//
// Flow: POST /v1/trades/:id/analyze → 202 with pending row OR 200 with
// cached success. FE polls GET /v1/trades/:id/analysis every ~3s until
// status flips from pending|generating → success|failed.

export type AnalysisStatus = 'pending' | 'generating' | 'success' | 'failed'

export interface Analysis {
  id: string
  tenant_id: string
  trade_id: string
  prompt_hash: string
  content: string | null
  tokens_in: number
  tokens_out: number
  model_id: string
  provider: string // 'stub' | 'openai' | 'anthropic'
  status: AnalysisStatus
  error: string | null
  requested_by_user_id: string
  requested_at: string
  completed_at: string | null
}

export interface Usage {
  day: string
  requests: number
  tokens_in: number
  tokens_out: number
  daily_budget: number
  hard_daily_cap: number
  headroom: number
  recap_requests?: number
  chat_requests?: number
  embedding_tokens?: number
}

// Phase 3 analytics bucket contracts. Numeric weekday/hour buckets are
// serialized as strings by the API, matching the shared backend shape.
export interface AnalyticsBucketRow {
  bucket: string
  trade_count: number
  wins: number
  losses: number
  breakevens: number
  net_p_l: number
  gross_profit: number
  gross_loss: number
  profit_factor: number
  win_rate: number
}

export type SessionRow = AnalyticsBucketRow
export type EARow = AnalyticsBucketRow
export type DayOfWeekRow = AnalyticsBucketRow
export type HourOfDayRow = AnalyticsBucketRow

export function useAnalysis() {
  /** Kick off (idempotent). Returns the analysis row (pending or cached). */
  async function analyze(tradeId: string): Promise<Analysis> {
    return await $fetch<Analysis>(`/v1/trades/${tradeId}/analyze`, {
      method: 'POST',
    })
  }

  /** Get the latest analysis for a trade. Returns null if 404. */
  async function getLatest(tradeId: string): Promise<Analysis | null> {
    try {
      return await $fetch<Analysis>(`/v1/trades/${tradeId}/analysis`)
    } catch (err: any) {
      if (err?.statusCode === 404) return null
      throw err
    }
  }

  /** Tenant's daily usage + budget. */
  async function getUsage(): Promise<Usage> {
    return await $fetch<Usage>('/v1/ai/usage')
  }

  async function getSession(accountId: string | null = null): Promise<{ sessions: SessionRow[] }> {
    return await $fetch<{ sessions: SessionRow[] }>('/v1/analytics/session', {
      query: accountId ? { account_id: accountId } : {},
    })
  }

  async function getEA(accountId: string | null = null): Promise<{ eas: EARow[] }> {
    return await $fetch<{ eas: EARow[] }>('/v1/analytics/ea', {
      query: accountId ? { account_id: accountId } : {},
    })
  }

  async function getDayOfWeek(accountId: string | null = null): Promise<{ weekdays: DayOfWeekRow[] }> {
    return await $fetch<{ weekdays: DayOfWeekRow[] }>('/v1/analytics/day-of-week', {
      query: accountId ? { account_id: accountId } : {},
    })
  }

  async function getHourOfDay(accountId: string | null = null): Promise<{ hours: HourOfDayRow[] }> {
    return await $fetch<{ hours: HourOfDayRow[] }>('/v1/analytics/hour-of-day', {
      query: accountId ? { account_id: accountId } : {},
    })
  }

  return { analyze, getLatest, getUsage, getSession, getEA, getDayOfWeek, getHourOfDay }
}
