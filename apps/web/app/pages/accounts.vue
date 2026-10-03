<script setup lang="ts">
// /accounts — list trading accounts (Phase 1 stub; future: edit/delete)
import { ref, onMounted } from 'vue'
import { useImports } from '~/composables/useImports'
import type { TradingAccount } from '@trading-journal/shared'

definePageMeta({ name: 'accounts' })

const { listAccounts } = useImports()
const accounts = ref<TradingAccount[]>([])
const loading = ref(true)

onMounted(async () => {
  try {
    const res = await listAccounts()
    accounts.value = res.accounts
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <v-container class="py-6">
    <h1 class="text-h4 mb-4">Trading Accounts</h1>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-card v-else-if="!accounts.length" variant="outlined" class="pa-12 text-center">
      <v-icon size="48" color="medium-emphasis" class="mb-2">mdi-account-multiple</v-icon>
      <div class="text-h6 mb-2">No trading accounts yet</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        Create your first account to start importing MT5 history.
      </div>
      <v-btn color="primary" to="/imports">Get started</v-btn>
    </v-card>

    <v-table v-else density="comfortable">
      <thead>
        <tr>
          <th>Display Name</th>
          <th>Broker</th>
          <th>Account ID</th>
          <th>Currency</th>
          <th>Kind</th>
          <th>Created</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="a in accounts" :key="a.id">
          <td class="font-weight-medium">{{ a.display_name }}</td>
          <td>{{ a.broker.toUpperCase() }}</td>
          <td>{{ a.broker_account_id }}</td>
          <td>{{ a.currency }}</td>
          <td><v-chip size="small" :color="a.kind === 'real' ? 'primary' : 'warning'">{{ a.kind }}</v-chip></td>
          <td class="text-caption">{{ new Date(a.created_at).toLocaleDateString() }}</td>
          <td>
            <v-btn variant="text" size="small" :to="`/imports?account_id=${a.id}`">Import</v-btn>
          </td>
        </tr>
      </tbody>
    </v-table>
  </v-container>
</template>