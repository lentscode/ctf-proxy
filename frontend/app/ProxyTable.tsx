import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getProxies, isUnauthorized, type ProxyView } from '../lib/api'

interface ProxyTableProps {
  onUnauthorized: () => void
}

const stateLabels: Record<ProxyView['state'], string> = {
  running: 'Running',
  inactive: 'Inactive',
  failed: 'Failed',
}

export function ProxyTable({ onUnauthorized }: ProxyTableProps) {
  const proxies = useQuery({
    queryKey: ['proxies'],
    queryFn: getProxies,
    refetchInterval: 10_000,
  })

  useEffect(() => {
    if (isUnauthorized(proxies.error)) {
      onUnauthorized()
    }
  }, [onUnauthorized, proxies.error])

  return (
    <section className="dashboard-panel proxy-panel" aria-labelledby="proxies-heading">
      <header className="panel-header">
        <h1 id="proxies-heading">Proxies</h1>
        {!proxies.isLoading && !proxies.isError && <span className="panel-count">{proxies.data?.length ?? 0}</span>}
      </header>
      <div className="panel-body">
        {proxies.isLoading && <ProxySkeleton />}
        {proxies.isError && !isUnauthorized(proxies.error) && (
          <div className="panel-message">
            <p>Unable to load proxies.</p>
            <button type="button" className="text-button" onClick={() => void proxies.refetch()}>Retry</button>
          </div>
        )}
        {proxies.data && proxies.data.length === 0 && <div className="panel-message">No proxies configured.</div>}
        {proxies.data && proxies.data.length > 0 && (
          <table className="proxy-table">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">State</th>
                <th scope="col">Protocol</th>
                <th scope="col">Listen</th>
              </tr>
            </thead>
            <tbody>
              {proxies.data.map((proxy) => <ProxyRow key={proxy.name} proxy={proxy} />)}
            </tbody>
          </table>
        )}
      </div>
    </section>
  )
}

function ProxyRow({ proxy }: { proxy: ProxyView }) {
  return (
    <tr>
      <th scope="row">{proxy.name}</th>
      <td><span className={`state-badge state-${proxy.state}`}>{stateLabels[proxy.state]}</span></td>
      <td className="mono-cell">{proxy.protocol}</td>
      <td className="mono-cell">{proxy.listen}</td>
    </tr>
  )
}

function ProxySkeleton() {
  return <div className="proxy-skeleton" aria-label="Loading proxies">{Array.from({ length: 5 }, (_, index) => <span key={index} />)}</div>
}
