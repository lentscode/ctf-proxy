import { Navigate, NavLink, Route, Routes } from 'react-router-dom'
import { Dashboard } from './Dashboard'
import { ProxiesPage } from './ProxiesPage'

interface AppShellProps {
  onUnauthorized: () => void
}

export function AppShell({ onUnauthorized }: AppShellProps) {
  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Primary navigation">
        <NavLink className="sidebar-brand" to="/">ctf-proxy</NavLink>
        <nav>
          <NavLink to="/" end>Dashboard</NavLink>
          <NavLink to="/proxies">Proxies</NavLink>
        </nav>
      </aside>
      <div className="app-content">
        <Routes>
          <Route path="/" element={<Dashboard onUnauthorized={onUnauthorized} />} />
          <Route path="/proxies" element={<ProxiesPage onUnauthorized={onUnauthorized} />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </div>
  )
}
