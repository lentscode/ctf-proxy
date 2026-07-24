const storageKey = 'ctf-proxy.auth-token'

// getAuthToken reads the session-scoped bearer token, if one is present.
export function getAuthToken(): string {
  return sessionStorage.getItem(storageKey) ?? ''
}

// saveAuthToken stores the bearer token only for the current browser session.
export function saveAuthToken(token: string): void {
  sessionStorage.setItem(storageKey, token)
}

// clearAuthToken removes the current session's bearer token.
export function clearAuthToken(): void {
  sessionStorage.removeItem(storageKey)
}

// authenticatedFetch adds the session bearer token before delegating to fetch.
export function authenticatedFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  const token = getAuthToken()
  const headers = new Headers(init.headers)

  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  return fetch(input, { ...init, headers })
}
