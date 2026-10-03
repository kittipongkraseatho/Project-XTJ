<script setup lang="ts">
// /imports — upload MT5 file + view import history
import { ref, computed, onMounted } from 'vue'
import { useImports } from '~/composables/useImports'
import type { ImportJob, TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'imports' })

const { uploadFile, listImports, listAccounts, createAccount } = useImports()

const file = ref<File | null>(null)
const selectedAccount = ref<string>('')
const uploading = ref(false)
const lastResult = ref<{ stats: { parsed: number; inserted: number; skipped: number; trade_count: number } } | null>(null)
const errorMsg = ref<string | null>(null)

const accounts = ref<TradingAccount[]>([])
const imports = ref<ImportJob[]>([])

// ── new-account dialog ─────────────────────────────────────────────────
const showAccountDialog = ref(false)
const newAcc = ref({ broker: 'mt5', broker_account_id: '', display_name: '', currency: 'USD', kind: 'real' })

async function refreshAll() {
  try {
    const a = await listAccounts()
    accounts.value = a.accounts
    if (a.accounts.length && !selectedAccount.value) selectedAccount.value = a.accounts[0].id
  } catch {}
  try {
    const i = await listImports(20)
    imports.value = i.imports
  } catch {}
}

onMounted(refreshAll)

function onFileChange(e: Event) {
  const t = e.target as HTMLInputElement
  file.value = t.files?.[0] ?? null
}

async function onUpload() {
  if (!file.value || !selectedAccount.value) return
  errorMsg.value = null
  lastResult.value = null
  uploading.value = true
  try {
    const res = await uploadFile(file.value, selectedAccount.value)
    lastResult.value = { stats: res.stats }
    file.value = null
    await refreshAll()
  } catch (err: any) {
    errorMsg.value = `${err?.code ?? 'ERROR'}: ${err?.message ?? 'upload failed'}`
  } finally {
    uploading.value = false
  }
}

async function onCreateAccount() {
  if (!newAcc.value.broker_account_id || !newAcc.value.display_name) return
  await createAccount(newAcc.value)
  showAccountDialog.value = false
  await refreshAll()
}

function statusColor(s: string) {
  switch (s) {
    case 'completed': return 'success'
    case 'failed': return 'error'
    case 'running': return 'info'
    case 'queued': return 'warning'
    default: return 'default'
  }
}
</script>

<template>
  <v-container class="py-8">
    <h1 class="text-h4 mb-6">Import MT5 history</h1>

    <v-row>
      <v-col cols="12" md="7">
        <v-card class="pa-6">
          <h2 class="text-h6 mb-4">Upload file</h2>

          <v-alert v-if="accounts.length === 0" type="info" variant="tonal" class="mb-4">
            You don't have any trading accounts yet. Create one below to get started.
          </v-alert>

          <v-select
            v-else
            v-model="selectedAccount"
            :items="accounts"
            item-title="display_name"
            item-value="id"
            label="Trading account"
            density="comfortable"
            class="mb-4"
          />

          <v-file-input
            v-model="file"
            label="MT5 export (.html or .csv)"
            accept=".html,.htm,.csv"
            prepend-icon="mdi-file-upload"
            density="comfortable"
            class="mb-4"
            @update:model-value="onFileChange"
          />

          <v-alert v-if="errorMsg" type="error" variant="tonal" class="mb-4" :text="errorMsg" />

          <v-alert v-if="lastResult" type="success" variant="tonal" class="mb-4">
            Import complete — parsed {{ lastResult.stats.parsed }} rows,
            inserted {{ lastResult.stats.inserted }},
            skipped {{ lastResult.stats.skipped }} (duplicates),
            derived {{ lastResult.stats.trade_count }} trades.
          </v-alert>

          <div class="d-flex ga-2">
            <v-btn color="primary" :loading="uploading" :disabled="!file || !selectedAccount" @click="onUpload">
              Upload & parse
            </v-btn>
            <v-btn variant="outlined" prepend-icon="mdi-plus" @click="showAccountDialog = true">
              New account
            </v-btn>
          </div>
        </v-card>
      </v-col>

      <v-col cols="12" md="5">
        <v-card class="pa-4">
          <h2 class="text-h6 mb-3">Recent imports</h2>
          <v-list v-if="imports.length" density="compact">
            <v-list-item v-for="imp in imports" :key="imp.id" :title="imp.imported_file_id">
              <template #prepend>
                <v-icon :color="statusColor(imp.status)">mdi-circle</v-icon>
              </template>
              <v-list-item-subtitle>
                {{ new Date(imp.created_at).toLocaleString() }} · {{ imp.status }}
                · {{ imp.inserted_rows }}/{{ imp.parsed_rows }} rows
              </v-list-item-subtitle>
            </v-list-item>
          </v-list>
          <div v-else class="text-medium-emphasis text-body-2 py-6 text-center">
            No imports yet — upload your first MT5 file to get started.
          </div>
        </v-card>
      </v-col>
    </v-row>

    <!-- New account dialog -->
    <v-dialog v-model="showAccountDialog" max-width="500">
      <v-card class="pa-6">
        <h2 class="text-h6 mb-4">New trading account</h2>
        <v-text-field v-model="newAcc.display_name" label="Display name" class="mb-3" />
        <v-text-field v-model="newAcc.broker_account_id" label="Broker account ID / login" class="mb-3" />
        <v-select v-model="newAcc.broker" :items="['mt5','mt4','ctrader','custom']" label="Broker" class="mb-3" />
        <v-text-field v-model="newAcc.currency" label="Currency" class="mb-3" />
        <v-select v-model="newAcc.kind" :items="['real','demo']" label="Kind" class="mb-3" />
        <div class="d-flex justify-end ga-2">
          <v-btn variant="text" @click="showAccountDialog = false">Cancel</v-btn>
          <v-btn color="primary" @click="onCreateAccount">Create</v-btn>
        </div>
      </v-card>
    </v-dialog>
  </v-container>
</template>