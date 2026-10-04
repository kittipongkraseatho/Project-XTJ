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
}

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

  return { analyze, getLatest, getUsage }
}