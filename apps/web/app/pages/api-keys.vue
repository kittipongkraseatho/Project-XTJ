<script setup lang="ts">
// /api-keys — Phase 5 API key management page.
//
// Shows the tenant's keys (prefix + meta, never the secret). The user can:
//   - create a new key (name + scopes + expiry) → plaintext shown ONCE
//   - revoke (soft-delete) an existing key
//   - update name/scopes/expiry
//
// The plaintext is rendered in a copy-to-clipboard box after creation.
// Closing the dialog without copying it is the user's responsibility.
import { ref, onMounted } from 'vue'
import {
  useApiKeys,
  ALL_API_SCOPES,
  READ_SCOPES,
  WRITE_SCOPES,
  API_SCOPES_META,
  type ApiKey,
  type ApiScope,
  type GenerateResult,
} from '~/composables/useApiKeys'

definePageMeta({ name: 'api-keys' })

const { list, create, revoke, update } = useApiKeys()

const keys = ref<ApiKey[]>([])
const loading = ref(true)
const errorMsg = ref<string | null>(null)
const showCreate = ref(false)
const busyId = ref<string | null>(null)

// Create form state
const newName = ref('')
const newScopes = ref<ApiScope[]>(['read:trades'])
const newExpiry = ref<'30d' | '90d' | '1y' | 'never'>('90d')
const createErr = ref<string | null>(null)
const creating = ref(false)

// One-shot plaintext display
const createdPlaintext = ref<string | null>(null)
const createdName = ref<string | null>(null)
const showPlaintext = ref(false)

async function refresh() {
  loading.value = true
  try {
    keys.value = await list()
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load keys'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  newName.value = ''
  newScopes.value = ['read:trades']
  newExpiry.value = '90d'
  createErr.value = null
  showCreate.value = true
}

async function onCreate() {
  if (!newName.value.trim()) {
    createErr.value = 'Name required'
    return
  }
  if (newScopes.value.length === 0) {
    createErr.value = 'Pick at least one scope'
    return
  }
  creating.value = true
  createErr.value = null
  try {
    const res: GenerateResult = await create({
      name: newName.value.trim(),
      scopes: newScopes.value,
      expires_in: newExpiry.value,
    })
    showCreate.value = false
    createdPlaintext.value = res.plaintext
    createdName.value = res.key.name
    showPlaintext.value = true
    await refresh()
  } catch (err: any) {
    createErr.value = err?.data?.error?.message ?? err?.message ?? 'Create failed'
  } finally {
    creating.value = false
  }
}

async function onRevoke(k: ApiKey) {
  if (k.revoked_at) return
  if (!confirm(`Revoke key "${k.name}"? Any client using it will get 401.`)) return
  busyId.value = k.id
  try {
    await revoke(k.id)
    await refresh()
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Revoke failed'
  } finally {
    busyId.value = null
  }
}

function maskedPrefix(prefix: string) {
  // trj_pk_abcd1234 — show as-is. Secret is never stored so we never show it.
  return prefix
}

async function copyPlaintext() {
  if (!createdPlaintext.value) return
  try {
    await navigator.clipboard.writeText(createdPlaintext.value)
  } catch {
    // Fall back: select-all in the textarea for manual copy.
    const ta = document.getElementById('plaintext-box') as HTMLTextAreaElement | null
    ta?.select()
  }
}

function fmtDate(s?: string | null) {
  if (!s) return '—'
  return new Date(s).toLocaleString()
}

function scopeChipColor(s: string): string {
  switch (s) {
    case 'read:accounts':
      return 'primary'
    case 'read:trades':
      return 'success'
    case 'read:analytics':
      return 'info'
    case 'read:payments':
      return 'warning'
    default:
      return 'grey'
  }
}

onMounted(refresh)
</script>

<template>
  <v-container class="py-6">
    <div class="d-flex align-center mb-4">
      <h1 class="text-h4">API Keys</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">
        Create API Key
      </v-btn>
    </div>

    <p class="text-body-2 text-medium-emphasis mb-4">
      Use these keys to access the read-only endpoints under <code>/v1/public/*</code>
      programmatically (e.g. from Zapier, a mobile client, or a custom dashboard).
      Treat the plaintext like a password — it is shown once and cannot be recovered.
    </p>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-alert v-else-if="errorMsg" type="error" :text="errorMsg" />

    <v-card v-else-if="!keys.length" variant="outlined" class="pa-4 text-center">
      <div class="text-body-2 text-medium-emphasis">
        No API keys yet. Click "Create API Key" to generate one.
      </div>
    </v-card>

    <v-card v-else variant="outlined">
      <v-table density="comfortable">
        <thead>
          <tr>
            <th>Name</th>
            <th>Prefix</th>
            <th>Scopes</th>
            <th>Last used</th>
            <th>Expires</th>
            <th>Status</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="k in keys" :key="k.id">
            <td>{{ k.name }}</td>
            <td>
              <code class="text-caption">{{ maskedPrefix(k.prefix) }}</code>
            </td>
            <td>
              <v-chip
                v-for="s in k.scopes"
                :key="s"
                size="x-small"
                :color="scopeChipColor(s)"
                class="mr-1"
              >
                {{ s }}
              </v-chip>
            </td>
            <td class="text-caption">{{ fmtDate(k.last_used_at) }}</td>
            <td class="text-caption">{{ fmtDate(k.expires_at) }}</td>
            <td>
              <v-chip
                v-if="k.revoked_at"
                size="x-small"
                color="error"
              >
                revoked
              </v-chip>
              <v-chip
                v-else-if="!k.is_active"
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
            <td class="text-right">
              <v-btn
                v-if="!k.revoked_at && k.is_active"
                :loading="busyId === k.id"
                size="x-small"
                color="error"
                variant="text"
                @click="onRevoke(k)"
              >
                Revoke
              </v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <!-- Create dialog -->
    <v-dialog v-model="showCreate" max-width="500">
      <v-card>
        <v-card-title>Create API Key</v-card-title>
        <v-card-text>
          <v-text-field
            v-model="newName"
            label="Name"
            placeholder="e.g. Zapier, Mobile app"
            variant="outlined"
            density="comfortable"
            class="mb-3"
          />
          <div class="text-subtitle-2 mb-1">Scopes</div>
          <div class="mb-3">
            <div class="text-caption text-medium-emphasis mb-1">Read</div>
            <v-checkbox
              v-for="s in READ_SCOPES"
              :key="s"
              v-model="newScopes"
              :value="s"
              :label="API_SCOPES_META.find(m => m.value === s)?.label || s"
              density="compact"
              hide-details
            />
            <v-divider class="my-2" />
            <div class="text-caption text-medium-emphasis mb-1">Write (Phase 10)</div>
            <v-checkbox
              v-for="s in WRITE_SCOPES"
              :key="s"
              v-model="newScopes"
              :value="s"
              :label="API_SCOPES_META.find(m => m.value === s)?.label || s"
              density="compact"
              hide-details
            />
            <v-btn
              size="x-small"
              variant="text"
              class="mt-2"
              @click="newScopes = WRITE_SCOPES"
            >Select all write scopes</v-btn>
          </div>
          <v-select
            v-model="newExpiry"
            :items="[
              { title: '30 days', value: '30d' },
              { title: '90 days', value: '90d' },
              { title: '1 year', value: '1y' },
              { title: 'Never', value: 'never' },
            ]"
            label="Expires"
            variant="outlined"
            density="comfortable"
          />
          <v-alert v-if="createErr" type="error" :text="createErr" class="mt-3" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showCreate = false">Cancel</v-btn>
          <v-btn color="primary" :loading="creating" @click="onCreate">Generate</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- One-shot plaintext display -->
    <v-dialog v-model="showPlaintext" max-width="600" persistent>
      <v-card>
        <v-card-title>
          <v-icon color="warning" class="mr-2">mdi-alert</v-icon>
          Save your API key
        </v-card-title>
        <v-card-text>
          <p class="text-body-2 mb-3">
            This is the only time we will show you the secret for
            <strong>{{ createdName }}</strong>. Copy it now and store it securely —
            we cannot recover it later.
          </p>
          <v-textarea
            id="plaintext-box"
            :model-value="createdPlaintext ?? ''"
            readonly
            rows="2"
            variant="outlined"
            density="comfortable"
          />
          <v-alert
            type="warning"
            class="mt-3"
            text="Anyone with this key can read your trading data until it expires or is revoked."
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