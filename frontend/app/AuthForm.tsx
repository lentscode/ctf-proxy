import type { FormEvent } from 'react'

interface AuthFormProps {
  token: string
  error?: string
  isConnecting: boolean
  onTokenChange: (token: string) => void
  onSubmit: (token: string) => void
}

export function AuthForm({ token, error, isConnecting, onTokenChange, onSubmit }: AuthFormProps) {
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onSubmit(token)
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <input
        aria-label="Control token"
        aria-describedby={error ? 'token-error' : undefined}
        aria-invalid={Boolean(error)}
        type="password"
        value={token}
        onChange={(event) => onTokenChange(event.target.value)}
        placeholder="Control token"
        autoComplete="off"
        disabled={isConnecting}
        required
      />
      {error && <p id="token-error" className="auth-error" role="alert">{error}</p>}
      <button type="submit" disabled={isConnecting}>
        {isConnecting ? 'Connecting…' : 'Continue'}
      </button>
    </form>
  )
}
