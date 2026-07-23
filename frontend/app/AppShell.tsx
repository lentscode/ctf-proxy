import { Navigate, NavLink, Route, Routes } from 'react-router-dom'
import { Dashboard } from './Dashboard'
import { ProxiesPage } from './ProxiesPage'

interface AppShellProps {
  onUnauthorized: () => void
}

export function AppShell({ onUnauthorized }: AppShellProps) {
  return (
    <div className="grid min-h-svh grid-cols-[214px_minmax(0,1fr)] bg-zinc-950 font-sans text-zinc-200 max-lg:flex max-lg:flex-col">
      <aside className="sticky top-0 flex h-svh flex-col self-start border-r border-zinc-700 p-4 pt-6 max-lg:static max-lg:h-auto max-lg:self-stretch max-lg:flex-none max-lg:flex-row max-lg:items-center max-lg:border-r-0 max-lg:border-b max-lg:px-4 max-lg:py-3.5" aria-label="Primary navigation">
        <NavLink className="px-2.5 pb-7 font-mono text-sm font-semibold text-zinc-100 no-underline max-lg:mr-auto max-lg:p-0" to="/">ctf-proxy</NavLink>
        <nav className="grid gap-0.5 max-lg:flex max-lg:flex-none max-lg:gap-1">
          <NavLink className={({ isActive }) => `border-l px-2.5 py-2.5 text-sm no-underline transition ${isActive ? 'border-zinc-100 bg-zinc-900 text-zinc-100' : 'border-transparent text-zinc-400 hover:border-zinc-100 hover:bg-zinc-900 hover:text-zinc-100'} max-lg:border-l-0 max-lg:border-b max-lg:px-2 max-lg:hover:border-b-zinc-100`} to="/" end>Dashboard</NavLink>
          <NavLink className={({ isActive }) => `border-l px-2.5 py-2.5 text-sm no-underline transition ${isActive ? 'border-zinc-100 bg-zinc-900 text-zinc-100' : 'border-transparent text-zinc-400 hover:border-zinc-100 hover:bg-zinc-900 hover:text-zinc-100'} max-lg:border-l-0 max-lg:border-b max-lg:px-2 max-lg:hover:border-b-zinc-100`} to="/proxies">Proxies</NavLink>
        </nav>
      </aside>
      <div className="min-w-0 max-lg:min-h-0">
        <Routes>
          <Route path="/" element={<Dashboard onUnauthorized={onUnauthorized} />} />
          <Route path="/proxies" element={<ProxiesPage onUnauthorized={onUnauthorized} />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </div>
  )
}
