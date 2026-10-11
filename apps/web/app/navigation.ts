export interface NavItem {
  title: string
  to: string
  enabled: boolean
}

export const NAV_ITEMS: NavItem[] = [
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
]
