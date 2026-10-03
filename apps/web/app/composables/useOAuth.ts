// useOAuth — start provider OAuth flow.
// Phase 0: just redirect to /v1/auth/oauth/:provider/start.

export function useOAuth() {
  function start(provider: 'google' | 'line' | 'github') {
    window.location.href = `/v1/auth/oauth/${provider}/start`
  }
  return { start }
}