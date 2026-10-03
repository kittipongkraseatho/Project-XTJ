// useBilling — Phase 4 billing composables.

export interface Plan {
  code: string
  name: string
  description: string
  price_cents: number
  currency: string
  interval: string
}

export interface SubscriptionDTO {
  plan_code: string
  plan_name: string
  status: string
  current_period_start: string
  current_period_end: string
  cancel_at?: string
  cancel_at_period_end: boolean
  external_subscription_id?: string
}

export interface Payment {
  id: string
  amount_cents: number
  currency: string
  status: string
  paid_at?: string
  created_at: string
}

export function useBilling() {
  async function listPlans() {
    return await $fetch<{ plans: Plan[] }>('/v1/billing/plans')
  }

  async function currentSubscription() {
    return await $fetch<SubscriptionDTO>('/v1/billing/subscription')
  }

  async function checkout(input: { plan_code: string; success_url: string; cancel_url: string }) {
    return await $fetch<{ checkout_url: string; session_id: string }>('/v1/billing/checkout', {
      method: 'POST',
      body: input,
    })
  }

  async function portal() {
    return await $fetch<{ portal_url: string }>('/v1/billing/portal')
  }

  async function cancel(atPeriodEnd = true) {
    return await $fetch<{ status: string; at_period_end: boolean }>('/v1/billing/cancel', {
      method: 'POST',
      body: { at_period_end: atPeriodEnd },
    })
  }

  async function listPayments() {
    return await $fetch<{ payments: Payment[] }>('/v1/billing/payments')
  }

  return {
    listPlans,
    currentSubscription,
    checkout,
    portal,
    cancel,
    listPayments,
  }
}