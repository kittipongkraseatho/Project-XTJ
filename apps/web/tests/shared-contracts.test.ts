import { describe, expect, it } from 'vitest'
import { API_SCOPES } from '../../../packages/shared/src/index'

describe('public API scope contract', () => {
  it('keeps the Phase 5 read scopes and Phase 10 write scopes grouped', () => {
    const readScopes = API_SCOPES.filter((scope) => scope.group === 'read').map((scope) => scope.value)
    const writeScopes = API_SCOPES.filter((scope) => scope.group === 'write').map((scope) => scope.value)

    expect(readScopes).toEqual([
      'read:accounts',
      'read:trades',
      'read:analytics',
      'read:payments',
    ])
    expect(writeScopes).toEqual([
      'write:trades',
      'write:journal',
      'write:webhooks',
      'write:accounts',
    ])
  })
})
