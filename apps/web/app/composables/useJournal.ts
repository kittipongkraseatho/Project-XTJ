// useJournal — trade journal entry CRUD.

interface JournalEntry {
  id: string
  trade_id: string
  strategy: string | null
  entry_reason: string | null
  exit_reason: string | null
  emotion: 'calm' | 'neutral' | 'fomo' | 'anxious' | 'confident' | 'fearful' | 'greedy' | null
  mistake: string | null
  notes: string | null
  updated_at: string
}

export function useJournal() {
  async function get(tradeId: string): Promise<JournalEntry | null> {
    try {
      return await $fetch<JournalEntry>(`/v1/trades/${tradeId}/journal`)
    } catch (err: any) {
      if (err?.status === 204 || err?.statusCode === 204) return null
      if (err?.status === 404) return null
      throw err
    }
  }

  async function upsert(tradeId: string, payload: Partial<JournalEntry>): Promise<JournalEntry> {
    return await $fetch<JournalEntry>(`/v1/trades/${tradeId}/journal`, {
      method: 'PUT',
      body: payload,
    })
  }

  return { get, upsert }
}