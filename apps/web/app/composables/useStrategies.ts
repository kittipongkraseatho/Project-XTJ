import { apiGet, apiPost, apiPut } from '~/composables/useApi'
import type { StrategyRuleSet } from './useStrategies-types'

export type StrategyStatus = 'draft' | 'active' | 'archived'

export interface Strategy {
  id: string
  name: string
  description: string
  status: StrategyStatus
  current_version_id?: string
  created_at: string
  updated_at: string
}

export interface StrategyVersion {
  id: string
  strategy_id: string
  version_no: number
  schema_version: number
  rules: StrategyRuleSet
  created_by: string
  created_at: string
}

export function useStrategies() {
  function list() {
    return apiGet<Strategy[]>('/v1/strategies')
  }

  function create(input: { name: string; description: string }) {
    return apiPost<Strategy>('/v1/strategies', input)
  }

  function listVersions(strategyId: string) {
    return apiGet<StrategyVersion[]>(`/v1/strategies/${strategyId}/versions`)
  }

  function createVersion(strategyId: string, rules: StrategyRuleSet) {
    return apiPost<StrategyVersion>(`/v1/strategies/${strategyId}/versions`, { rules })
  }

  function linkTrade(tradeId: string, strategyVersionId: string) {
    return apiPut<void>(`/v1/trades/${tradeId}/strategy-version`, {
      strategy_version_id: strategyVersionId,
    })
  }

  return { list, create, listVersions, createVersion, linkTrade }
}
