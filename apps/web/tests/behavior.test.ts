import { describe, expect, it } from 'vitest'
import { percent } from '../app/composables/behavior-utils'

describe('behavior presentation helpers', () => {
  it('formats share values as whole percentages', () => {
    expect(percent(0.7)).toBe('70%')
    expect(percent(0)).toBe('0%')
  })
})
