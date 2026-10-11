import { describe, expect, it } from 'vitest'
import { defaultStrategyRules } from '../app/composables/strategy-utils'

describe('strategy editor defaults', () => {
  it('starts with a valid descriptive rule shape', () => {
    const rules = defaultStrategyRules()
    expect(rules.schema_version).toBe(1)
    expect(rules.entry.length).toBeGreaterThan(0)
    expect(rules.exit.length).toBeGreaterThan(0)
    expect(rules.risk.max_volume).toBeGreaterThan(0)
  })
})
