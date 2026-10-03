<script setup lang="ts">
// /webhooks — Phase 5.2 webhook subscription management page.
//
// Shows the tenant's subscriptions. The user can:
//   - create a new subscription (URL + events + description) → plaintext secret shown ONCE
//   - revoke (soft-delete) an existing subscription
//   - click row → drill-down panel with recent deliveries

import { ref, computed, onMounted } from 'vue'
import {
  useWebhooks,
  ALL_WEBHOOK_EVENTS,
  type WebhookSubscription,
  type WebhookEventType,
  type WebhookCreateResult,
  type WebhookDelivery,
} from '~/composables/useWebhooks'

definePageMeta({ name: 'webhooks' })

const { list, create, revoke, listDeliveries } = useWebhooks()

const subs = ref<WebhookSubscription[]>([])
const loading = ref(true)
const errorMsg = ref<string | null>(null)
const busyId = ref<string | null>(null)

// Create form
const showCreate = ref(false)
const newUrl = ref('')
const newDescription = ref('')
const newEvents = ref<WebhookEventType[]>(['trade.created'])
const createBusy = ref(false)
const createErr = ref<string | null>(null)

// One-shot plaintext display
const createdPlaintext = ref<string | null>(null)
const createdDescription = ref<string | null>(null)
const showPlaintext = ref(false)

// Drill-down
const drillId = ref<string | null>(null)
const drillDeliveries = ref<WebhookDelivery[]>([])
const drillLoading = ref(false)

async function refresh() {
  loading.value = true
  try {
    subs.value = await list()
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load webhooks'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  newUrl.value = ''
  newDescription.value = ''
  newEvents.value = ['trade.created']
  createErr.value = null
  showCreate.value = true
}

async function onCreate() {
  if (!newUrl.value.trim()) {
    createErr.value = 'URL required'
    return
  }
  if (newEvents.value.length === 0) {
    createErr.value = 'Pick at least one event'
    return
  }
  createBusy.value = true
  createErr.value = null
  try {
    const res: WebhookCreateResult = await create({
      url: newUrl.value.trim(),
      event_types: newEvents.value,
      description: newDescription.value.trim(),
    })
    showCreate.value = false
    createdPlaintext.value = res.plaintext
    createdDescription.value = res.subscription.description || res.subscription.url
    showPlaintext.value = true
    await refresh()
  } catch (err: any) {
    createErr.value = err?.data?.error?.message ?? err?.message ?? 'Create failed'
  } finally {
    createBusy.value = false
  }
}

async function onRevoke(s: WebhookSubscription) {
  if (s.revoked_at) return
  if (!confirm(`Revoke webhook "${s.description || s.url}"? Any receiver will stop getting events immediately.`)) return
  busyId.value = s.id
  try {
    await revoke(s.id)
    if (drillId.value === s.id) {
      drillId.value = null
    }
    await refresh()
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Revoke failed'
  } finally {
    busyId.value = null
  }
}

async function copyPlaintext() {
  if (!createdPlaintext.value) return
  try {
    await navigator.clipboard.writeText(createdPlaintext.value)
  } catch {
    const ta = document.getElementById('wh-plaintext-box') as HTMLTextAreaElement | null
    ta?.select()
  }
}

async function openDrill(s: WebhookSubscription) {
  if (drillId.value === s.id) {
    drillId.value = null
    return
  }
  drillId.value = s.id
  drillLoading.value = true
  try {
    drillDeliveries.value = await listDeliveries(s.id)
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? 'Failed to load deliveries'
  } finally {
    drillLoading.value = false
  }
}

function fmtDate(s?: string | null) {
  if (!s) return '—'
  return new Date(s).toLocaleString()
}

function statusColor(status: string): string {
  switch (status) {
    case 'success': return 'success'
    case 'pending': return 'info'
    case 'failed': return 'error'
    case 'exhausted': return 'warning'
    default: return 'grey'
  }
}

const hasDrill = computed(() => drillId.value !== null)

onMounted(refresh)
</script>

<template>
  <v-container class="py-6">
    <div class="d-flex align-center mb-4">
      <h1 class="text-h4">Webhooks</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">
        Create Webhook
      </v-btn>
    </div>

    <p class="text-body-2 text-medium-emphasis mb-4">
      Webhooks let your services receive HTTP POSTs when events happen in Trading Journal —
      e.g. when a trade is created from an import. The plaintext signing secret is shown
      once at creation; store it on your receiver to verify HMAC signatures.
    </p>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-alert v-else-if="errorMsg" type="error" :text="errorMsg" />

    <v-card v-else-if="!subs.length" variant="outlined" class="pa-4 text-center">
      <div class="text-body-2 text-medium-emphasis">
        No webhook subscriptions yet. Click "Create Webhook" to set one up.
      </div>
    </v-card>

    <v-card v-else variant="outlined">
      <v-table density="comfortable">
        <thead>
          <tr>
            <th>URL</th>
            <th>Events</th>
            <th>Description</th>
            <th>Status</th>
            <th>Created</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="s in subs"
            :key="s.id"
            :class="{ 'bg-grey-darken-3': drillId === s.id }"
            style="cursor: pointer"
            @click="openDrill(s)"
          >
            <td>
              <code class="text-caption">{{ s.url }}</code>
            </td>
            <td>
              <v-chip
                v-for="e in s.event_types"
                :key="e"
                size="x-small"
                class="mr-1"
              >
                {{ e }}
              </v-chip>
            </td>
            <td class="text-body-2">{{ s.description || '—' }}</td>
            <td>
              <v-chip
                v-if="s.revoked_at"
                size="x-small"
                color="error"
              >
                revoked
              </v-chip>
              <v-chip
                v-else-if="!s.is_active"
                size="x-small"
                color="grey"
              >
                inactive
              </v-chip>
              <v-chip
                v-else
                size="x-small"
                color="success"
              >
                active
              </v-chip>
            </td>
            <td class="text-caption">{{ fmtDate(s.created_at) }}</td>
            <td class="text-right" @click.stop>
              <v-btn
                v-if="!s.revoked_at && s.is_active"
                :loading="busyId === s.id"
                size="x-small"
                color="error"
                variant="text"
                @click="onRevoke(s)"
              >
                Revoke
              </v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <!-- Drill-down: recent deliveries for the selected subscription -->
    <v-card v-if="hasDrill" variant="outlined" class="mt-4 pa-4">
      <div class="d-flex align-center mb-2">
        <h3 class="text-h6">Recent deliveries</h3>
        <v-spacer />
        <v-btn variant="text" size="small" @click="drillId = null">Close</v-btn>
      </div>
      <v-progress-circular v-if="drillLoading" indeterminate color="primary" size="small" />
      <v-table v-else-if="drillDeliveries.length" density="compact">
        <thead>
          <tr>
            <th>Created</th>
            <th>Event</th>
            <th>Status</th>
            <th>Attempt</th>
            <th>Response</th>
            <th>Last tried</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in drillDeliveries" :key="d.id">
            <td class="text-caption">{{ fmtDate(d.created_at) }}</td>
            <td class="text-caption">{{ d.event_type }}</td>
            <td>
              <v-chip size="x-small" :color="statusColor(d.status)">{{ d.status }}</v-chip>
            </td>
            <td class="text-caption">{{ d.attempt }} / {{ d.max_attempts }}</td>
            <td class="text-caption">
              <span v-if="d.response_code">{{ d.response_code }}</span>
              <span v-else class="text-disabled">—</span>
              <span v-if="d.error" class="text-caption text-error ml-1">({{ d.error }})</span>
            </td>
            <td class="text-caption">{{ fmtDate(d.last_attempted_at) }}</td>
          </tr>
        </tbody>
      </v-table>
      <div v-else class="text-body-2 text-medium-emphasis text-center pa-2">
        No deliveries yet. Events fire automatically as the system processes jobs.
      </div>
    </v-card>

    <!-- Create dialog -->
    <v-dialog v-model="showCreate" max-width="500">
      <v-card>
        <v-card-title>Create Webhook</v-card-title>
        <v-card-text>
          <v-text-field
            v-model="newUrl"
            label="URL"
            placeholder="https://example.com/webhook"
            variant="outlined"
            density="comfortable"
            class="mb-3"
          />
          <v-text-field
            v-model="newDescription"
            label="Description (e.g. 'Zapier', 'Mobile app')"
            variant="outlined"
            density="comfortable"
            class="mb-3"
          />
          <div class="text-subtitle-2 mb-1">Events</div>
          <div class="mb-2">
            <v-checkbox
              v-for="e in ALL_WEBHOOK_EVENTS"
              :key="e"
              v-model="newEvents"
              :value="e"
              :label="e"
              density="compact"
              hide-details
            />
          </div>
          <v-alert v-if="createErr" type="error" :text="createErr" class="mt-3" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showCreate = false">Cancel</v-btn>
          <v-btn color="primary" :loading="createBusy" @click="onCreate">Generate</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- One-shot plaintext display -->
    <v-dialog v-model="showPlaintext" max-width="600" persistent>
      <v-card>
        <v-card-title>
          <v-icon color="warning" class="mr-2">mdi-alert</v-icon>
          Save your signing secret
        </v-card-title>
        <v-card-text>
          <p class="text-body-2 mb-3">
            This is the only time we will show you the signing secret for
            <strong>{{ createdDescription }}</strong>. Copy it now and store it on your
            receiver to verify HMAC-SHA256 signatures.
          </p>
          <v-textarea
            id="wh-plaintext-box"
            :model-value="createdPlaintext ?? ''"
            readonly
            rows="2"
            variant="outlined"
            density="comfortable"
          />
          <v-alert
            type="info"
            class="mt-3"
            text="Headers sent on every delivery: X-Webhook-Id, X-Webhook-Event, X-Webhook-Timestamp, X-Webhook-Signature: sha256=<hex>, X-Webhook-Attempt."
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" prepend-icon="mdi-content-copy" @click="copyPlaintext">
            Copy
          </v-btn>
          <v-btn color="primary" @click="showPlaintext = false">I have saved it</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>