import { EventStream } from './EventStream'
import { ProxyTable } from './ProxyTable'

interface DashboardProps {
  onUnauthorized: () => void
}

export function Dashboard({ onUnauthorized }: DashboardProps) {
  return (
    <main className="dashboard-shell">
      <div className="metrics-reservation" aria-hidden="true" />
      <div className="dashboard-grid">
        <ProxyTable onUnauthorized={onUnauthorized} />
        <EventStream onUnauthorized={onUnauthorized} />
      </div>
    </main>
  )
}
