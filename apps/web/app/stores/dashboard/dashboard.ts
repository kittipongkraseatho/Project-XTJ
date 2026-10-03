// useDashboardStore — Pinia store backing the multi-account dashboard (Phase 3).
//
// State:
//   selectedAccountId — UUID of the trading account currently focused, or null
//                     for the "All accounts" aggregate view. The dashboard
//                     reads this and passes it as ?account_id= to the BE.

import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useDashboardStore = defineStore('dashboard', () => {
  const selectedAccountId = ref<string | null>(null)

  function setSelectedAccount(id: string | null) {
    selectedAccountId.value = id
  }

  return { selectedAccountId, setSelectedAccount }
})