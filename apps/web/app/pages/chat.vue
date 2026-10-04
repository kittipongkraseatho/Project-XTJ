<script setup lang="ts">
// /chat — Phase 9 multi-turn Q&A chat with RAG.
//
// Sidebar: thread list (most recent) + new-thread button.
// Main: message stream (user/assistant bubbles) + composer.
// Account-scope selector scopes RAG retrieval to one account.

import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useChat, type ChatThread, type ChatMessage } from '~/composables/useChat'
import { useImports } from '~/composables/useImports'
import { useAnalysis, type Usage } from '~/composables/useAnalysis'
import type { TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'chat' })

const { listThreads, createThread, deleteThread, listMessages, sendMessage, pollForReply } = useChat()
const { listAccounts } = useImports()
const { getUsage } = useAnalysis()

const threads = ref<ChatThread[]>([])
const selectedThreadId = ref<string>('')
const messages = ref<ChatMessage[]>([])
const accounts = ref<TradingAccount[]>([])
const scopeAccountId = ref<string | null>(null)
const newThreadTitle = ref<string>('')
const composer = ref<string>('')
const sending = ref(false)
const errorMsg = ref<string | null>(null)
const usage = ref<Usage | null>(null)
const pendingTyping = ref(false)
const messagesEnd = ref<HTMLElement | null>(null)

async function refreshThreads() {
  try {
    threads.value = await listThreads()
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load threads'
  }
}

async function refreshMessages() {
  if (!selectedThreadId.value) {
    messages.value = []
    return
  }
  try {
    messages.value = await listMessages(selectedThreadId.value)
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load messages'
  }
}

async function refreshUsage() {
  try {
    usage.value = await getUsage()
  } catch {
    // Ignore — usage is best-effort in the chat header.
  }
}

function pickThread(t: ChatThread) {
  selectedThreadId.value = t.id
}

async function onNewThread() {
  try {
    const t = await createThread({
      accountId: scopeAccountId.value ?? undefined,
      title: newThreadTitle.value || 'New chat',
    })
    newThreadTitle.value = ''
    await refreshThreads()
    pickThread(t)
    await refreshMessages()
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to create thread'
  }
}

async function onDeleteThread(id: string) {
  if (!confirm('Delete this thread and all messages?')) return
  try {
    await deleteThread(id)
    if (selectedThreadId.value === id) {
      selectedThreadId.value = ''
    }
    await refreshThreads()
    await refreshMessages()
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to delete thread'
  }
}

async function onSend() {
  const text = composer.value.trim()
  if (!text || !selectedThreadId.value || sending.value) return
  sending.value = true
  pendingTyping.value = true
  try {
    const userMsg = await sendMessage(selectedThreadId.value, text)
    composer.value = ''
    await refreshMessages()
    await scrollToBottom()
    const reply = await pollForReply(selectedThreadId.value, userMsg.id, { intervalMs: 1500, maxMs: 45_000 })
    if (reply) {
      await refreshMessages()
      await refreshUsage()
    } else {
      errorMsg.value = 'Assistant reply timed out — try again'
    }
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to send message'
  } finally {
    sending.value = false
    pendingTyping.value = false
  }
}

async function scrollToBottom() {
  await nextTick()
  messagesEnd.value?.scrollIntoView({ behavior: 'smooth' })
}

watch(selectedThreadId, async () => {
  await refreshMessages()
  await scrollToBottom()
})

onMounted(async () => {
  const [a] = await Promise.all([listAccounts(), refreshThreads(), refreshUsage()])
  accounts.value = a.accounts
  if (threads.value.length > 0) {
    selectedThreadId.value = threads.value[0].id
    await refreshMessages()
  }
})

const budgetText = computed(() => {
  if (!usage.value) return ''
  return `${usage.value.requests} / ${usage.value.daily_budget} used today · headroom ${usage.value.headroom}`
})
</script>

<template>
  <v-container fluid>
    <v-row>
      <v-col cols="12">
        <h1 class="text-h4 mb-2">AI Chat</h1>
        <p class="text-medium-emphasis mb-4">Ask questions about your trading history. RAG retrieves your top-10 most relevant trades to feed the LLM.</p>
      </v-col>
    </v-row>

    <v-row v-if="errorMsg">
      <v-col cols="12">
        <v-alert type="error" closable @click:close="errorMsg = null">{{ errorMsg }}</v-alert>
      </v-col>
    </v-row>

    <v-row>
      <v-col cols="12" md="3">
        <v-card>
          <v-card-title class="d-flex align-center">
            <span>Threads</span>
            <v-spacer />
            <v-btn icon="mdi-plus" size="small" variant="text" @click="onNewThread" title="New thread" />
          </v-card-title>
          <v-card-text>
            <v-text-field
              v-model="newThreadTitle"
              label="New thread title"
              density="compact"
              variant="outlined"
              hide-details
              class="mb-2"
            />
            <v-select
              v-model="scopeAccountId"
              :items="[{ title: 'All accounts', value: null }, ...accounts.map(a => ({ title: a.display_name, value: a.id }))]"
              item-title="title"
              item-value="value"
              label="Scope"
              density="compact"
              variant="outlined"
              hide-details
              class="mb-3"
            />
            <v-list v-if="threads.length > 0" density="compact">
              <v-list-item
                v-for="t in threads"
                :key="t.id"
                :active="t.id === selectedThreadId"
                @click="pickThread(t)"
              >
                <v-list-item-title class="text-truncate" style="max-width: 200px">{{ t.title || 'Untitled' }}</v-list-item-title>
                <v-list-item-subtitle class="text-caption">{{ new Date(t.last_message_at).toLocaleString() }}</v-list-item-subtitle>
                <template #append>
                  <v-btn icon="mdi-delete" size="x-small" variant="text" @click.stop="onDeleteThread(t.id)" />
                </template>
              </v-list-item>
            </v-list>
            <div v-else class="text-caption text-medium-emphasis">No threads yet.</div>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="9">
        <v-card class="d-flex flex-column" style="height: 70vh">
          <v-card-title class="d-flex align-center">
            <span>{{ threads.find(t => t.id === selectedThreadId)?.title || 'Select a thread' }}</span>
            <v-spacer />
            <span v-if="usage" class="text-caption text-medium-emphasis">{{ budgetText }}</span>
          </v-card-title>

          <v-card-text class="flex-grow-1 overflow-y-auto" style="background: rgba(0,0,0,0.05)">
            <div v-if="messages.length === 0" class="text-center text-medium-emphasis pa-4">
              {{ selectedThreadId ? 'No messages yet. Ask a question below.' : 'Pick a thread from the sidebar or create a new one.' }}
            </div>
            <div v-for="m in messages" :key="m.id" class="mb-3 d-flex" :class="m.role === 'user' ? 'justify-end' : 'justify-start'">
              <v-card
                :color="m.role === 'user' ? 'primary' : 'surface-variant'"
                :class="m.role === 'user' ? 'text-right' : ''"
                max-width="70%"
                class="pa-3"
              >
                <div class="text-caption text-medium-emphasis mb-1">
                  {{ m.role === 'user' ? 'You' : 'Assistant' }}
                  <span v-if="m.provider"> · {{ m.provider }}</span>
                </div>
                <div style="white-space: pre-wrap">{{ m.content }}</div>
              </v-card>
            </div>
            <div v-if="pendingTyping" class="mb-3 d-flex justify-start">
              <v-card color="surface-variant" class="pa-3">
                <v-progress-circular indeterminate size="16" width="2" />
                <span class="ml-2 text-caption text-medium-emphasis">Assistant is typing…</span>
              </v-card>
            </div>
            <div ref="messagesEnd" />
          </v-card-text>

          <v-card-actions>
            <v-text-field
              v-model="composer"
              placeholder="Ask about your trades…"
              density="compact"
              variant="outlined"
              hide-details
              :disabled="!selectedThreadId || sending"
              @keydown.enter.prevent="onSend"
            />
            <v-btn
              color="primary"
              :loading="sending"
              :disabled="!selectedThreadId || !composer.trim() || sending"
              @click="onSend"
            >
              Send
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>