import { useEffect, useState } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { useMutation, useQuery } from '@tanstack/react-query'
import { applyScanConfiguration, getManagedDeployments, isUnauthorized, restoreManagedDeployments, type ScanCandidate, type ScanDiscovery } from '../lib/api'
import { queryClient } from '../lib/query-client'
import { toast } from 'sonner'
import { defaultProtocolChoice, type ProtocolChoice } from './protocol-choice'

interface Props {
  discovery: UseQueryResult<ScanDiscovery, Error>
  onClose: () => void
  onUnauthorized: () => void
  open: boolean
}

type Choice = ProtocolChoice

// ScanAndConfigureModal lets an operator review a fresh project scan before it changes Docker state.
export function ScanAndConfigureModal({ discovery, onClose, onUnauthorized, open }: Props) {
  const [choices, setChoices] = useState<Record<string, Choice>>({})
  const [confirmation, setConfirmation] = useState(false)
  const deployments = useQuery({ queryKey: ['managed-deployments'], queryFn: getManagedDeployments, enabled: open })
  const apply = useMutation({
    mutationFn: () => {
      if (!discovery.data) throw new Error('Wait for the project scan before applying changes.')
      const selections = Object.entries(choices)
        .filter(([, choice]) => choice.selected)
        .map(([id, choice]) => ({ id, protocol: choice.protocol, ...(choice.protocol === 'http' ? { scheme: choice.scheme } : {}) }))
      return applyScanConfiguration(discovery.data.revision, selections)
    },
    onSuccess: async () => {
      setConfirmation(false)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['managed-deployments'] }),
        queryClient.invalidateQueries({ queryKey: ['proxies'] }),
      ])
      toast.success('Configuration applied', { description: 'Selected services are now managed by ctf-proxy.' })
    },
    onError: (error) => { if (!isUnauthorized(error)) toast.error('Could not apply configuration', { description: error instanceof Error ? error.message : 'Try again in a moment.' }) },
  })
  const restore = useMutation({
    mutationFn: ({ ids, all }: { ids: string[], all: boolean }) => restoreManagedDeployments(ids, all),
    onSuccess: async (_, { all }) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['managed-deployments'] }),
        queryClient.invalidateQueries({ queryKey: ['proxies'] }),
      ])
      toast.success(all ? 'Deployments restored' : 'Deployment restored', { description: 'The original Compose port mappings were restored.' })
    },
    onError: (error) => { if (!isUnauthorized(error)) toast.error('Could not restore deployment', { description: error instanceof Error ? error.message : 'Try again in a moment.' }) },
  })

  useEffect(() => {
    if ([discovery.error, deployments.error, apply.error, restore.error].some(isUnauthorized)) onUnauthorized()
  }, [apply.error, deployments.error, discovery.error, onUnauthorized, restore.error])
  useEffect(() => {
    if (!open) return
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === 'Escape' && !apply.isPending && !restore.isPending) onClose() }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [apply.isPending, onClose, open, restore.isPending])

  const selectedCount = Object.values(choices).filter((choice) => choice.selected).length
  const update = (candidate: ScanCandidate, value: Partial<Choice>) => setChoices((current) => {
    const choice = current[candidate.id] ?? defaultProtocolChoice(candidate)
    return { ...current, [candidate.id]: { ...choice, ...value } }
  })

  if (!open) return null
  return <div className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !apply.isPending && !restore.isPending) onClose() }}>
    <section className="max-h-[calc(100svh-2rem)] w-full max-w-5xl overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-950 shadow-2xl" role="dialog" aria-modal="true" aria-labelledby="scan-and-configure-heading">
      <header className="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-zinc-700 bg-zinc-950 px-6 py-5">
        <div><p className="m-0 font-mono text-[11px] tracking-[.08em] text-zinc-400 uppercase">Attack & Defense CTF</p><h2 id="scan-and-configure-heading" className="mt-1.5 mb-0 text-2xl font-semibold text-zinc-100">Scan and configure</h2><p className="mb-0 text-sm text-zinc-400">Review proposed private upstream mappings before applying them.</p></div>
        <button type="button" className="min-h-9 rounded-md border border-zinc-600 px-3 text-sm font-semibold text-zinc-100 hover:border-zinc-100 disabled:opacity-60" onClick={onClose} disabled={apply.isPending || restore.isPending}>Close</button>
      </header>
      <div className="grid gap-7 p-6">
        <section aria-labelledby="scan-results-heading"><div className="flex items-center justify-between gap-4"><div><h3 id="scan-results-heading" className="m-0 text-lg font-semibold text-zinc-100">Project scan</h3><p className="mb-0 text-sm text-zinc-400">Only explicit externally published TCP ports can be selected. The replacement address is loopback-only.</p></div><button type="button" className="min-h-9 rounded-md border border-zinc-600 px-3 text-sm font-semibold text-zinc-100 hover:border-zinc-100 disabled:opacity-60" onClick={() => void discovery.refetch()} disabled={discovery.isFetching}>{discovery.isFetching ? 'Scanning…' : 'Scan again'}</button></div>
          {discovery.isLoading && <p className="mt-4 text-sm text-zinc-400">Scanning projects…</p>}
          {discovery.isError && !isUnauthorized(discovery.error) && <p className="mt-4 text-sm text-zinc-400" role="alert">Unable to scan projects. Try again in a moment.</p>}
          {discovery.data?.projects.length === 0 && <p className="mt-4 text-sm text-zinc-400">No Compose projects were found.</p>}
          {discovery.data && <div className="mt-5 grid gap-5">{discovery.data.projects.map((project) => <article key={project.compose_file} className="rounded-md border border-zinc-700"><header className="border-b border-zinc-700 px-4 py-3"><h4 className="m-0 text-sm font-semibold text-zinc-100">{project.name}</h4><p className="m-0 font-mono text-[11px] text-zinc-400">{project.compose_file}</p></header>{project.candidates.map((candidate) => { const choice = choices[candidate.id] ?? defaultProtocolChoice(candidate); return <div key={candidate.id} className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-center gap-3 border-b border-zinc-800 px-4 py-3 last:border-b-0 max-md:grid-cols-1"><input aria-label={`Select ${candidate.service} ${candidate.listen}`} type="checkbox" checked={choice.selected} disabled={!candidate.eligible} onChange={(event) => update(candidate, { selected: event.target.checked })}/><div><p className="m-0 text-sm text-zinc-100">{candidate.service} · <span className="font-mono text-xs">{candidate.listen} → {candidate.upstream || candidate.target}</span></p>{candidate.reason && <p className="m-0 text-xs text-zinc-400">Skipped: {candidate.reason}</p>}</div>{candidate.eligible && <select aria-label={`Protocol for ${candidate.service} ${candidate.listen}`} value={choice.protocol} onChange={(event) => update(candidate, { protocol: event.target.value as 'tcp' | 'http' })} disabled={!choice.selected} className="h-9 rounded border border-zinc-600 bg-zinc-950 px-2 text-sm text-zinc-100"><option value="tcp">TCP</option><option value="http">HTTP</option></select>}{candidate.eligible && choice.protocol === 'http' && <select aria-label={`Scheme for ${candidate.service} ${candidate.listen}`} value={choice.scheme} onChange={(event) => update(candidate, { scheme: event.target.value as 'http' | 'https' })} disabled={!choice.selected} className="h-9 rounded border border-zinc-600 bg-zinc-950 px-2 text-sm text-zinc-100"><option value="http">HTTP</option><option value="https">HTTPS</option></select>}</div> })}</article>)}</div>}
          {discovery.data && <div className="mt-5 flex flex-wrap items-center gap-3"><button type="button" className="min-h-9 rounded-md border border-zinc-600 px-3 text-sm font-semibold text-zinc-100 hover:border-zinc-100 disabled:opacity-50" disabled={selectedCount === 0 || apply.isPending} onClick={() => setConfirmation(true)}>Apply {selectedCount || ''} selected</button>{confirmation && <div className="flex flex-wrap items-center gap-2 text-sm text-zinc-300"><span>This recreates affected services and starts proxies.</span><button type="button" className="rounded border border-zinc-400 px-2 py-1 font-semibold" onClick={() => void apply.mutateAsync()}>Confirm</button><button type="button" className="underline" onClick={() => setConfirmation(false)}>Cancel</button></div>}</div>}
        </section>
        <section className="border-t border-zinc-700 pt-7" aria-labelledby="managed-heading"><h3 id="managed-heading" className="m-0 text-lg font-semibold text-zinc-100">Managed deployments</h3><p className="text-sm text-zinc-400">Restoration is refused if a managed Compose file changed after configuration.</p>{deployments.isLoading && <p className="text-sm text-zinc-400">Loading managed deployments…</p>}{deployments.data?.length === 0 && <p className="text-sm text-zinc-400">No managed deployments.</p>}{deployments.data && deployments.data.length > 0 && <div className="grid gap-2">{deployments.data.map((deployment) => <div key={deployment.id} className="flex items-center justify-between gap-4 rounded-md border border-zinc-700 p-3 max-sm:flex-col max-sm:items-start"><div><p className="m-0 text-sm text-zinc-100">{deployment.project} / {deployment.service} · <span className="font-mono text-xs">{deployment.listen} → {deployment.upstream}</span></p><p className="m-0 text-xs text-zinc-400">{deployment.proxy} · {deployment.protocol.toUpperCase()} · {deployment.state}</p></div><button type="button" className="rounded border border-zinc-600 px-2 py-1 text-sm text-zinc-100 disabled:opacity-50" disabled={deployment.state === 'drifted' || restore.isPending} onClick={() => { if (window.confirm(`Restore ${deployment.project}/${deployment.service}?`)) void restore.mutate({ ids: [deployment.id], all: false }) }}>Restore</button></div>)}</div>}{deployments.data && deployments.data.length > 0 && <button type="button" className="mt-4 rounded-md border border-zinc-600 px-3 py-2 text-sm font-semibold text-zinc-100 disabled:opacity-50" disabled={restore.isPending} onClick={() => { if (window.confirm('Restore every managed deployment?')) void restore.mutate({ ids: [], all: true }) }}>Restore all</button>}</section>
      </div>
    </section>
  </div>
}
