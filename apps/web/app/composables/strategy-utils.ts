import type { StrategyRuleSet } from './useStrategies-types'

export function defaultStrategyRules(): StrategyRuleSet {
  return {
    schema_version: 1,
    setup: { symbols: ['EURUSD'], directions: ['long'] },
    entry: [{ field: 'rsi', operator: 'lt', value: 30 }],
    exit: [{ field: 'rsi', operator: 'gt', value: 70 }],
    risk: { stop_distance: 10, target_distance: 20, max_volume: 1 },
  }
}
