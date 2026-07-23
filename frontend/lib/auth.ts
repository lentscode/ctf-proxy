const storageKey = 'ctf-proxy.auth-token'

export function getAuthToken(): string {
  return sessionStorage.getItem(storageKey) ?? ''
}

export function saveAuthToken(token: string): void {
  sessionStorage.setItem(storageKey, token)
}

export function clearAuthToken(): void {
  sessionStorage.removeItem(storageKey)
}

export function authenticatedFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  const token = getAuthToken()
  const headers = new Headers(init.headers)

  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  return fetch(input, { ...init, headers })
}
