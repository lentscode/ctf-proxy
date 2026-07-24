import type { FormEvent } from 'react'

// AuthFormProps describes the token form's controlled state and submit action.
interface AuthFormProps {
  token: string
  error?: string
  isConnecting: boolean
  onTokenChange: (token: string) => void
  onSubmit: (token: string) => void
}

// AuthForm collects the loopback API bearer token without exposing it in plain text.
export function AuthForm({ token, error, isConnecting, onTokenChange, onSubmit }: AuthFormProps) {
  // submit keeps the form interaction from triggering a document navigation.
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onSubmit(token)
  }

  return (
    <form className="flex w-full max-w-sm flex-col gap-3 rounded-2xl border border-zinc-700 bg-zinc-900 p-7 shadow-2xl shadow-black/25 sm:p-10" onSubmit={submit}>
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
        className="h-12 w-full rounded-lg border border-zinc-600 bg-zinc-950 px-3 font-mono text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10 aria-invalid:border-zinc-200 disabled:cursor-wait disabled:opacity-65"
      />
      {error && <p id="token-error" className="-mt-0.5 text-sm text-zinc-200" role="alert">{error}</p>}
      <button type="submit" disabled={isConnecting} className="h-12 cursor-pointer rounded-lg border border-zinc-200 bg-zinc-200 font-bold text-zinc-950 transition hover:bg-white disabled:cursor-wait disabled:opacity-75">
        {isConnecting ? 'Connecting…' : 'Continue'}
      </button>
    </form>
  )
}
