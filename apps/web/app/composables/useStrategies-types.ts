export interface StrategyRuleSet {
  schema_version: 1
  setup: { symbols: string[]; directions: string[]; sessions?: string[] }
  entry: Array<{ field: string; operator: string; value: unknown }>
  exit: Array<{ field: string; operator: string; value: unknown }>
  risk: { stop_distance: number; target_distance: number; max_volume: number }
}
