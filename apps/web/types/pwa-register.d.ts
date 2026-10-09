declare module 'virtual:pwa-register/vue' {
  interface RegisterSWOptions {
    onNeedRefresh?: () => void
    onOfflineReady?: () => void
    onRegistered?: (registration: ServiceWorkerRegistration | undefined) => void
    onRegisterError?: (error: unknown) => void
  }

  interface RegisterSWResult {
    needRefresh: Readonly<{ value: boolean }>
    offlineReady: Readonly<{ value: boolean }>
    updateServiceWorker: (reloadPage?: boolean) => Promise<void>
  }

  export function useRegisterSW(options?: RegisterSWOptions): RegisterSWResult
}
