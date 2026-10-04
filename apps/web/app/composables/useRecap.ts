// useRecap — Phase 9 AI weekly recap composable.
//
// Flow: POST /v1/accounts/:id/recaps → 202 (pending) OR 200 (cached success).
// FE polls GET /v1/recaps/:id every ~3s until status flips to success|failed.

export type RecapStatus = 'pending' | 'generating' | 'success' | 'failed'

export interface Recap {
  id: string
  tenant_id: string
  account_id: string
  week_start: string // YYYY-MM-DD
  week_end: string // YYYY-MM-DD
  prompt_hash: string
  content: string | null
  tokens_in: number
  tokens_out: number
  model_id: string
  provider: string
  status: RecapStatus
  error: string | null
  requested_by_user_id: string
  requested_at: string
  completed_at: string | null
}

export function useRecap() {
  /** Kick off a recap (idempotent — returns cached if recent). */
  async function generate(accountId: string, weekStart?: string): Promise<Recap> {
    return await $fetch<Recap>(`/v1/accounts/${accountId}/recaps`, {
      method: 'POST',
      body: weekStart ? { week_start: weekStart } : {},
    })
  }

  /** List recaps for an account (newest first). */
  async function list(accountId: string): Promise<Recap[]> {
    return await $fetch<Recap[]>(`/v1/accounts/${accountId}/recaps`)
  }

  /** Get one recap by id. */
  async function get(recapId: string): Promise<Recap> {
    return await $fetch<Recap>(`/v1/recaps/${recapId}`)
  }

  /** Poll helper — returns the recap once it lands on success|failed, or null on timeout. */
  async function pollUntilDone(recapId: string, opts: { intervalMs?: number; maxMs?: number } = {}): Promise<Recap | null> {
    const interval = opts.intervalMs ?? 3000
    const max = opts.maxMs ?? 60_000
    const started = Date.now()
    while (Date.now() - started < max) {
      const r = await get(recapId)
      if (r.status === 'success' || r.status === 'failed') return r
      await new Promise((resolve) => setTimeout(resolve, interval))
    }
    return null
  }

  return { generate, list, get, pollUntilDone }
}