// useWebhooks — Phase 5.2 webhook subscription admin composables.
//
// All endpoints are JWT-authed. The plaintext secret is returned
// EXACTLY ONCE at creation — the FE must show it to the user and
// discard.

export type WebhookEventType = 'import.completed' | 'trade.created'

export const ALL_WEBHOOK_EVENTS: WebhookEventType[] = [
  'import.completed',
  'trade.created',
]

export interface WebhookSubscription {
  id: string
  tenant_id: string
  url: string
  event_types: string[]
  description: string
  is_active: boolean
  created_by_user_id: string
  created_at: string
  updated_at: string
  revoked_at?: string | null
}

// CreateResult is what POST /v1/webhooks returns. plaintext is shown once.
export interface WebhookCreateResult {
  subscription: WebhookSubscription
  plaintext: string // trj_whsec_<64-hex>
}

export interface WebhookDelivery {
  id: string
  tenant_id: string
  subscription_id: string
  event_type: string
  event_id: string
  attempt: number
  max_attempts: number
  status: 'pending' | 'success' | 'failed' | 'exhausted'
  response_code?: number | null
  response_body?: string | null
  error?: string | null
  next_attempt_at: string
  last_attempted_at?: string | null
  completed_at?: string | null
  created_at: string
}

export function useWebhooks() {
  async function list(): Promise<WebhookSubscription[]> {
    const res = await $fetch<{ webhooks: WebhookSubscription[] }>('/v1/webhooks')
    return res.webhooks
  }

  async function create(input: {
    url: string
    event_types: WebhookEventType[]
    description?: string
  }): Promise<WebhookCreateResult> {
    return await $fetch<WebhookCreateResult>('/v1/webhooks', {
      method: 'POST',
      body: input,
    })
  }

  async function update(
    id: string,
    input: {
      url?: string
      event_types?: WebhookEventType[]
      description?: string
      is_active?: boolean
    },
  ): Promise<WebhookSubscription> {
    const res = await $fetch<{ webhook: WebhookSubscription }>(`/v1/webhooks/${id}`, {
      method: 'PATCH',
      body: input,
    })
    return res.webhook
  }

  async function revoke(id: string): Promise<void> {
    await $fetch<null>(`/v1/webhooks/${id}`, { method: 'DELETE' })
  }

  async function listDeliveries(id: string): Promise<WebhookDelivery[]> {
    const res = await $fetch<{ deliveries: WebhookDelivery[] }>(
      `/v1/webhooks/${id}/deliveries`,
    )
    return res.deliveries
  }

  return { list, create, update, revoke, listDeliveries }
}