// useApiKeys — Phase 5 API key admin composables.
//
// All endpoints are JWT-authed (Bearer access token), distinct from the
// public /v1/public/* surface (which uses API keys to authenticate).
//
// The plaintext API key is returned EXACTLY ONCE at creation. The FE must
// show it to the user and discard; the BE never persists the plaintext.

export type ApiScope =
  | 'read:accounts'
  | 'read:trades'
  | 'read:analytics'
  | 'read:payments'

export const ALL_API_SCOPES: ApiScope[] = [
  'read:accounts',
  'read:trades',
  'read:analytics',
  'read:payments',
]

export interface ApiKey {
  id: string
  tenant_id: string
  name: string
  prefix: string // e.g. trj_pk_abcd1234 (no secret)
  scopes: string[]
  last_used_at?: string | null
  expires_at?: string | null
  is_active: boolean
  created_by_user_id: string
  created_at: string
  revoked_at?: string | null
}

// GenerateResult is what POST /v1/api-keys returns. plaintext is shown once.
export interface GenerateResult {
  key: ApiKey
  plaintext: string
}

export function useApiKeys() {
  async function list(): Promise<ApiKey[]> {
    const res = await $fetch<{ keys: ApiKey[] }>('/v1/api-keys')
    return res.keys
  }

  async function create(input: {
    name: string
    scopes: ApiScope[]
    expires_in?: '30d' | '90d' | '1y' | 'never'
  }): Promise<GenerateResult> {
    return await $fetch<GenerateResult>('/v1/api-keys', {
      method: 'POST',
      body: input,
    })
  }

  async function revoke(id: string): Promise<void> {
    await $fetch<null>(`/v1/api-keys/${id}`, { method: 'DELETE' })
  }

  async function update(
    id: string,
    input: { name?: string; scopes?: ApiScope[]; expires_in?: '30d' | '90d' | '1y' | 'never' },
  ): Promise<ApiKey> {
    const res = await $fetch<{ key: ApiKey }>(`/v1/api-keys/${id}`, {
      method: 'PATCH',
      body: input,
    })
    return res.key
  }

  return { list, create, revoke, update }
}