// useTrades — Phase 1 trades list/detail composables.

import type { Trade } from '@trading-journal/shared'

export function useTrades() {
  async function list(opts: { accountId?: string; limit?: number; offset?: number } = {}) {
    return await $fetch<{ trades: Trade[]; total: number }>('/v1/trades', {
      query: {
        ...(opts.accountId ? { account_id: opts.accountId } : {}),
        limit: opts.limit ?? 50,
        offset: opts.offset ?? 0,
      },
    })
  }

  async function get(id: string): Promise<Trade> {
    return await $fetch<Trade>(`/v1/trades/${id}`)
  }

  return { list, get }
}
