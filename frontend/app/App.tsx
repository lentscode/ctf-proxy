import { useState } from 'react'
import type { FormEvent } from 'react'
import { getAuthToken, saveAuthToken } from '../lib/auth'
import './App.css'

function App() {
  const [token, setToken] = useState(() => getAuthToken())

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    saveAuthToken(token.trim())
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <input
        aria-label="Control token"
        type="password"
        value={token}
        onChange={(event) => setToken(event.target.value)}
        placeholder="Control token"
        autoComplete="off"
      />
      <button type="submit">Continue</button>
    </form>
  )
}

export default App
