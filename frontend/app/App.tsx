import { useCallback, useEffect, useRef, useState } from 'react'
import { AuthForm } from './AuthForm'
import { AppShell } from './AppShell'
import { isUnauthorized, verifyHealth } from '../lib/api'
import { clearAuthToken, getAuthToken, saveAuthToken } from '../lib/auth'

// App owns the token handshake and switches between authentication and the dashboard.
function App() {
  const [token, setToken] = useState(() => getAuthToken())
  const [authenticated, setAuthenticated] = useState(false)
  const [isConnecting, setIsConnecting] = useState(false)
  const [authError, setAuthError] = useState<string | undefined>()
  const initialToken = useRef(getAuthToken())

  // disconnect clears local credentials after an API rejects the current token.
  const disconnect = useCallback(() => {
    clearAuthToken()
    setToken('')
    setAuthenticated(false)
    setIsConnecting(false)
    setAuthError('Token was not accepted.')
  }, [])

  // connect persists a candidate token only after using it to verify the API.
  const connect = useCallback(async (candidate: string) => {
    const nextToken = candidate.trim()
    if (!nextToken) {
      return
    }

    saveAuthToken(nextToken)
    setToken(nextToken)
    setAuthError(undefined)
    setIsConnecting(true)

    try {
      await verifyHealth()
      setAuthenticated(true)
    } catch (error) {
      if (isUnauthorized(error)) {
        clearAuthToken()
        setToken('')
        setAuthError('Token was not accepted.')
      } else {
        setAuthError('Unable to reach ctf-proxy.')
      }
      setAuthenticated(false)
    } finally {
      setIsConnecting(false)
    }
  }, [])

  useEffect(() => {
    if (initialToken.current) {
      void connect(initialToken.current)
    }
  }, [connect])

  if (!authenticated) {
    return (
      <main className="grid min-h-svh place-items-center bg-zinc-950 p-4 font-sans text-zinc-200">
        <AuthForm
          token={token}
          error={authError}
          isConnecting={isConnecting}
          onTokenChange={setToken}
          onSubmit={connect}
        />
      </main>
    )
  }

  return <AppShell onUnauthorized={disconnect} />
}

export default App
