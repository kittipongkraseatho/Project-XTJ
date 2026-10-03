// useImports — Phase 1 upload + list composables.

import type { ImportJob, TradeDTO, TradingAccount } from '@trading-journal/shared'

interface ImportResult {
  imported_file: { id: string; original_filename: string; format: string; byte_size: number }
  import_job: ImportJob
  trades: TradeDTO[]
  stats: { parsed: number; inserted: number; skipped: number; trade_count: number }
}

export function useImports() {
  async function uploadFile(file: File, tradingAccountId: string): Promise<ImportResult> {
    const form = new FormData()
    form.append('file', file)
    form.append('trading_account_id', tradingAccountId)
    return await $fetch<ImportResult>('/v1/imports', {
      method: 'POST',
      body: form,
    })
  }

  async function listImports(limit = 20) {
    return await $fetch<{ imports: ImportJob[] }>('/v1/imports', {
      query: { limit },
    })
  }

  async function listAccounts() {
    return await $fetch<{ accounts: TradingAccount[] }>('/v1/accounts')
  }

  async function createAccount(input: {
    broker: string
    broker_account_id: string
    display_name: string
    currency?: string
    kind?: string
  }) {
    return await $fetch<TradingAccount>('/v1/accounts', {
      method: 'POST',
      body: input,
    })
  }

  return { uploadFile, listImports, listAccounts, createAccount }
}