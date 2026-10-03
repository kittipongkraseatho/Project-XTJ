// useSchedules — Phase 4 email scheduling composables.

export interface ReportSchedule {
  id: string
  tenant_id: string
  trading_account_id: string
  frequency: 'daily' | 'weekly' | 'monthly'
  hour_utc: number
  weekday?: number
  day_of_month?: number
  last_sent_at?: string
  next_send_at: string
  recipients: string[]
  is_active: boolean
  created_at: string
  updated_at: string
}

export function useSchedules() {
  async function list() {
    return await $fetch<{ schedules: ReportSchedule[] }>('/v1/reports/schedules')
  }

  async function create(input: {
    trading_account_id: string
    frequency: 'daily' | 'weekly' | 'monthly'
    hour_utc: number
    weekday?: number
    day_of_month?: number
    recipients?: string[]
  }) {
    return await $fetch<ReportSchedule>('/v1/reports/schedules', {
      method: 'POST',
      body: input,
    })
  }

  async function remove(id: string) {
    return await $fetch<null>(`/v1/reports/schedules/${id}`, { method: 'DELETE' })
  }

  return { list, create, remove }
}