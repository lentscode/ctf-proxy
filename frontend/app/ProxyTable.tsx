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
    <section className="relative flex min-w-0 flex-col overflow-hidden" aria-labelledby="proxies-heading">
      <header className="flex min-h-15 items-center border-b border-zinc-700 px-5">
        <h1 id="proxies-heading" className="text-base font-semibold tracking-tight text-zinc-100">Proxies</h1>
        {!proxies.isLoading && !proxies.isError && <span className="ml-2 grid size-5 place-items-center rounded-full border border-zinc-700 font-mono text-xs text-zinc-400">{proxies.data?.length ?? 0}</span>}
      </header>
      <div className="min-h-0 flex-1 overflow-auto">
        {proxies.isLoading && <ProxySkeleton />}
        {proxies.isError && !isUnauthorized(proxies.error) && (
          <div className="grid min-h-45 place-content-center gap-3 p-6 text-center text-sm text-zinc-400">
            <p>Unable to load proxies.</p>
            <button type="button" className="justify-self-center bg-transparent p-0 text-sm text-zinc-200 underline underline-offset-3" onClick={() => void proxies.refetch()}>Retry</button>
          </div>
        )}
        {proxies.data && proxies.data.length === 0 && <div className="grid min-h-45 place-content-center p-6 text-center text-sm text-zinc-400">No proxies configured.</div>}
        {proxies.data && proxies.data.length > 0 && (
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="sticky top-0 z-1 bg-zinc-950 text-left text-[11px] font-semibold tracking-[0.07em] text-zinc-400 uppercase">
                <th scope="col" className="h-10 border-b border-zinc-700 px-3">Name</th>
                <th scope="col" className="h-10 border-b border-zinc-700 px-3">State</th>
                <th scope="col" className="h-10 border-b border-zinc-700 px-3 max-sm:hidden">Protocol</th>
                <th scope="col" className="h-10 border-b border-zinc-700 px-3">Listen</th>
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
    <tr className="border-b border-zinc-700 text-sm hover:bg-zinc-900">
      <th scope="row" className="h-14 px-3 font-medium">{proxy.name}</th>
      <td className="h-14 px-3"><span className="inline-flex rounded-full bg-zinc-800 px-2 py-1 text-[11px] leading-none font-semibold text-zinc-200">{stateLabels[proxy.state]}</span></td>
      <td className="h-14 px-3 font-mono text-xs max-sm:hidden">{proxy.protocol}</td>
      <td className="h-14 px-3 font-mono text-xs">{proxy.listen}</td>
    </tr>
  )
}

function ProxySkeleton() {
  return <div className="grid gap-px" aria-label="Loading proxies">{Array.from({ length: 5 }, (_, index) => <span key={index} className="h-14 animate-pulse bg-zinc-900" />)}</div>
}
