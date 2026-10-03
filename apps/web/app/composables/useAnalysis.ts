// useAnalysis — Phase 3 advanced analytics composables.

export interface SessionRow {
  bucket: 'asia' | 'london' | 'newyork' | 'sydney'
  trade_count: number
  wins: number
  losses: number
  breakevens: number
  net_p_l: number
  gross_profit: number
  gross_loss: number
  win_rate: number
  profit_factor: number
}

export interface EARow {
  bucket: string // magic as string ("0" for manual, "12345" for EA)
  trade_count: number
  wins: number
  losses: number
  breakevens: number
  net_p_l: number
  gross_profit: number
  gross_loss: number
  win_rate: number
  profit_factor: number
}

export interface DayOfWeekRow {
  bucket: string // "0".."6" (Sunday=0)
  trade_count: number
  wins: number
  losses: number
  breakevens: number
  net_p_l: number
  gross_profit: number
  gross_loss: number
  win_rate: number
  profit_factor: number
}

export interface HourOfDayRow {
  bucket: string // "0".."23"
  trade_count: number
  wins: number
  losses: number
  breakevens: number
  net_p_l: number
  gross_profit: number
  gross_loss: number
  win_rate: number
  profit_factor: number
}

export function useAnalysis() {
  async function getSession(accountID: string | null = null) {
    return await $fetch<{ sessions: SessionRow[] }>('/v1/analytics/session', {
      query: accountID ? { account_id: accountID } : {},
    })
  }

  async function getEA(accountID: string | null = null) {
    return await $fetch<{ eas: EARow[] }>('/v1/analytics/ea', {
      query: accountID ? { account_id: accountID } : {},
    })
  }

  async function getDayOfWeek(accountID: string | null = null) {
    return await $fetch<{ weekdays: DayOfWeekRow[] }>('/v1/analytics/day-of-week', {
      query: accountID ? { account_id: accountID } : {},
    })
  }

  async function getHourOfDay(accountID: string | null = null) {
    return await $fetch<{ hours: HourOfDayRow[] }>('/v1/analytics/hour-of-day', {
      query: accountID ? { account_id: accountID } : {},
    })
  }

  return { getSession, getEA, getDayOfWeek, getHourOfDay }
}