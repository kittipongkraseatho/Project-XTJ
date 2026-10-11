import { describe, expect, it } from 'vitest'
import { NAV_ITEMS } from '../app/navigation'

describe('authenticated navigation availability', () => {
  it('enables only release-ready product areas', () => {
    expect(NAV_ITEMS).toEqual([
      { title: 'Dashboard', to: '/dashboard', enabled: true },
      { title: 'Trades', to: '/trades', enabled: true },
      { title: 'Imports', to: '/imports', enabled: true },
      { title: 'Analytics', to: '/analytics', enabled: true },
      { title: 'Accounts', to: '/accounts', enabled: true },
      { title: 'Behavior', to: '/behavior', enabled: true },
      { title: 'Strategies', to: '/strategies', enabled: true },
      { title: 'Recaps', to: '/recaps', enabled: false },
      { title: 'Chat', to: '/chat', enabled: false },
      { title: 'API Keys', to: '/api-keys', enabled: false },
      { title: 'Webhooks', to: '/webhooks', enabled: false },
    ])
  })
})
