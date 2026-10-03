// useTrades — Phase 1 trades list/detail composables.

interface Trade {
  id: string
  symbol: string
  direction: 'long' | 'short'
  open_time: string
  close_time: string | null
  volume: number
  open_price: number
  close_price: number | null
  gross_profit: number
  gross_loss: number
  net_p_l: number
  outcome: 'win' | 'loss' | 'breakeven'
  is_partial: boolean
}

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