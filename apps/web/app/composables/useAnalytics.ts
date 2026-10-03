// useAnalytics — Phase 1 analytics composables.

interface AnalyticsOverview {
  trade_count: number
  net_p_l: number
  gross_profit: number
  gross_loss: number
  wins: number
  losses: number
  win_rate: number
  profit_factor: number
  avg_win: number
  avg_loss: number
  expectancy: number
}

interface DailyMetric {
  trade_date: string
  trade_count: number
  net_p_l: number
}

export function useAnalytics() {
  async function getOverview(accountId?: string): Promise<AnalyticsOverview> {
    return await $fetch<AnalyticsOverview>('/v1/analytics/overview', {
      query: accountId ? { account_id: accountId } : {},
    })
  }

  async function getDailyPL(from: string, to: string, accountId?: string) {
    return await $fetch<{ daily: DailyMetric[] }>('/v1/analytics/daily-pl', {
      query: { from, to, ...(accountId ? { account_id: accountId } : {}) },
    })
  }

  return { getOverview, getDailyPL }
}