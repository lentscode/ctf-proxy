import { EventStream } from './EventStream'
import { ProxyTable } from './ProxyTable'

// DashboardProps contains the callback used when an API request loses authorization.
interface DashboardProps {
  onUnauthorized: () => void
}

// Dashboard combines the proxy summary with the live operational event stream.
export function Dashboard({ onUnauthorized }: DashboardProps) {
  return (
    <main className="mx-auto w-full max-w-[1440px] px-8 pb-8 max-lg:px-6 max-lg:pb-6 max-sm:px-4 max-sm:pb-4">
      <div className="h-40 max-lg:h-24" aria-hidden="true" />
      <div className="grid grid-cols-2 max-lg:grid-cols-1 max-lg:gap-8">
        <ProxyTable onUnauthorized={onUnauthorized} />
        <EventStream onUnauthorized={onUnauthorized} />
      </div>
    </main>
  )
}
