// useChat — Phase 9 AI Q&A chat composable.
//
// Flow: POST /v1/chat/threads/:id/messages → 202 (pending user msg).
// Worker handles assistant reply. FE polls GET /v1/chat/threads/:id/messages
// every ~2s until assistant message arrives (new messages count grows).

export interface ChatThread {
  id: string
  tenant_id: string
  user_id: string
  account_id: string | null
  title: string
  created_at: string
  last_message_at: string
}

export interface ChatMessage {
  id: string
  tenant_id: string
  thread_id: string
  role: 'user' | 'assistant'
  content: string
  tokens_in: number
  tokens_out: number
  model_id: string | null
  provider: string | null
  context_trade_ids: string[] | null
  requested_at: string
}

export function useChat() {
  async function listThreads(): Promise<ChatThread[]> {
    return await $fetch<ChatThread[]>('/v1/chat/threads')
  }

  async function createThread(input: { accountId?: string; title?: string } = {}): Promise<ChatThread> {
    return await $fetch<ChatThread>('/v1/chat/threads', {
      method: 'POST',
      body: {
        account_id: input.accountId,
        title: input.title,
      },
    })
  }

  async function deleteThread(threadId: string): Promise<void> {
    await $fetch(`/v1/chat/threads/${threadId}`, { method: 'DELETE' })
  }

  async function listMessages(threadId: string): Promise<ChatMessage[]> {
    return await $fetch<ChatMessage[]>(`/v1/chat/threads/${threadId}/messages`)
  }

  async function sendMessage(threadId: string, content: string): Promise<ChatMessage> {
    return await $fetch<ChatMessage>(`/v1/chat/threads/${threadId}/messages`, {
      method: 'POST',
      body: { content },
    })
  }

  /** Poll until an assistant reply appears after the userMessageId. */
  async function pollForReply(
    threadId: string,
    userMessageId: string,
    opts: { intervalMs?: number; maxMs?: number } = {},
  ): Promise<ChatMessage | null> {
    const interval = opts.intervalMs ?? 2000
    const max = opts.maxMs ?? 30_000
    const started = Date.now()
    while (Date.now() - started < max) {
      const all = await listMessages(threadId)
      // Find first assistant message after userMessageId (by requested_at order).
      let seenUser = false
      for (const m of all) {
        if (m.id === userMessageId) {
          seenUser = true
          continue
        }
        if (seenUser && m.role === 'assistant') {
          return m
        }
      }
      await new Promise((resolve) => setTimeout(resolve, interval))
    }
    return null
  }

  return { listThreads, createThread, deleteThread, listMessages, sendMessage, pollForReply }
}