import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { createProxy, deleteProxy, getFilters, getProxies, isUnauthorized, proxyDefinitionSchema, replaceProxy, type FilterView, type ProxyDefinition, type ProxyView } from '../lib/api'
import { queryClient } from '../lib/query-client'

// ProxiesPageProps contains the callback used when any management request expires.
interface ProxiesPageProps {
  onUnauthorized: () => void
}

const emptyProxy: ProxyDefinition = {
  name: '', active: true, protocol: 'tcp', listen: '', upstream: '', filters: [],
}

// ProxiesPage manages proxy selection, editing, creation, and deletion.
export function ProxiesPage({ onUnauthorized }: ProxiesPageProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedName, setSelectedName] = useState<string | undefined>()
  const [isCreating, setIsCreating] = useState(false)
  const focusedOnce = useRef<string | undefined>(undefined)
  const proxies = useQuery({ queryKey: ['proxies'], queryFn: getProxies })
  const filters = useQuery({ queryKey: ['filters'], queryFn: getFilters })
  const requestedProxy = searchParams.get('proxy') ?? undefined
  const selectedProxyName = requestedProxy && proxies.data?.some((proxy) => proxy.name === requestedProxy) ? requestedProxy : selectedName
  const selected = proxies.data?.find((proxy) => proxy.name === selectedProxyName)
  const showEditor = selected !== undefined || isCreating || proxies.data?.length === 0

  // refresh invalidates the shared list so all proxy views converge after a mutation.
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['proxies'] })
  }
  const create = useMutation({ mutationFn: createProxy, onSuccess: refresh })
  const replace = useMutation({ mutationFn: ({ name, definition }: { name: string, definition: ProxyDefinition }) => replaceProxy(name, definition), onSuccess: refresh })
  const remove = useMutation({ mutationFn: deleteProxy, onSuccess: async () => { setSelectedName(undefined); setIsCreating(false); setSearchParams({}); await refresh() } })

  useEffect(() => {
    if (isUnauthorized(proxies.error) || isUnauthorized(filters.error) || isUnauthorized(create.error) || isUnauthorized(replace.error) || isUnauthorized(remove.error)) {
      onUnauthorized()
    }
  }, [create.error, filters.error, onUnauthorized, proxies.error, remove.error, replace.error])

  useEffect(() => {
    if (!requestedProxy || selected?.name !== requestedProxy || focusedOnce.current === requestedProxy) return
    const heading = document.getElementById('proxy-editor-heading')
    if (!heading) return
    focusedOnce.current = requestedProxy
    heading.scrollIntoView({ behavior: 'smooth', block: 'start' })
    heading.focus({ preventScroll: true })
  }, [requestedProxy, selected?.name])

  function selectProxy(name: string | undefined) {
    setSelectedName(name)
    setIsCreating(name === undefined)
    if (requestedProxy) setSearchParams({})
  }

  // save chooses create or replacement based on the current selection.
  async function save(definition: ProxyDefinition) {
    if (selected) {
      await replace.mutateAsync({ name: selected.name, definition })
      setSelectedName(selected.name)
    } else {
      const created = await create.mutateAsync(definition)
      setSelectedName(created.name)
      setIsCreating(false)
    }
  }

  // confirmDelete requires an explicit browser confirmation before removal.
  function confirmDelete() {
    if (selected && window.confirm(`Remove proxy “${selected.name}”?`)) {
      remove.mutate(selected.name)
    }
  }

  const mutationError = [create.error, replace.error, remove.error].find(Boolean)
  return (
    <main className="mx-auto w-full max-w-[1440px] px-8 pt-14 pb-8 max-lg:px-6 max-lg:pt-10 max-lg:pb-6 max-sm:px-4 max-sm:pt-8 max-sm:pb-4">
      <header className="flex min-h-26 items-end justify-between gap-4 border-b border-zinc-700 pb-6 max-sm:min-h-0 max-sm:items-start">
        <div><p className="m-0 font-mono text-[11px] leading-none tracking-[0.08em] text-zinc-400 uppercase">Configuration</p><h1 className="mt-1.5 mb-0 text-3xl font-semibold tracking-tight text-zinc-100">Proxies</h1></div>
        <button type="button" className="min-h-9 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900" onClick={() => selectProxy(undefined)}>Add proxy</button>
      </header>
      <div className="grid min-h-140 grid-cols-[minmax(250px,0.8fr)_minmax(0,1.2fr)] max-lg:min-h-0 max-lg:grid-cols-1">
        <section className="border-r border-zinc-700 p-6 max-lg:border-r-0 max-lg:border-b" aria-labelledby="configured-proxies">
          <h2 id="configured-proxies" className="m-0 mb-5 text-base font-semibold text-zinc-100">Configured proxies</h2>
          {proxies.isLoading && <p className="m-0 text-sm text-zinc-400">Loading proxies…</p>}
          {proxies.isError && !isUnauthorized(proxies.error) && <p className="m-0 text-sm text-zinc-400">Unable to load proxies.</p>}
          {proxies.data?.length === 0 && <p className="m-0 text-sm text-zinc-400">No proxies configured.</p>}
          <div className="-mx-6 grid">
            {proxies.data?.map((proxy) => <ProxyDirectoryItem key={proxy.name} proxy={proxy} selected={selectedProxyName === proxy.name} onSelect={() => selectProxy(proxy.name)} />)}
          </div>
        </section>
        {showEditor && <section className="p-6" aria-labelledby="proxy-editor-heading">
          <h2 id="proxy-editor-heading" tabIndex={-1} className="m-0 mb-5 text-base font-semibold text-zinc-100 outline-none">{selected ? `Edit ${selected.name}` : 'Add proxy'}</h2>
          {mutationError && !isUnauthorized(mutationError) && <p className="m-0 mb-5 text-sm text-zinc-200">Unable to save this proxy. Check the values and try again.</p>}
          <ProxyEditor
            key={selected?.name ?? 'new'}
            initial={selected ? toDefinition(selected) : emptyProxy}
            isExisting={Boolean(selected)}
            isSaving={create.isPending || replace.isPending}
            onSave={save}
            onDelete={selected ? confirmDelete : undefined}
            isDeleting={remove.isPending}
            filters={filters.data ?? []}
            filtersUnavailable={filters.isError}
            assignments={filterAssignments(proxies.data ?? [])}
          />
        </section>}
      </div>
    </main>
  )
}

// ProxyDirectoryItem is the selectable summary row for one configured proxy.
function ProxyDirectoryItem({ proxy, selected, onSelect }: { proxy: ProxyView, selected: boolean, onSelect: () => void }) {
  return (
    <div className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-t border-zinc-700 px-6 py-3.5 transition ${selected ? 'bg-zinc-900 shadow-[inset_2px_0_0_0_#f4f4f5]' : 'bg-transparent hover:bg-zinc-900'}`}>
      <button type="button" className="grid cursor-pointer gap-1 text-left" onClick={onSelect}><span className="text-sm text-zinc-100">{proxy.name}</span><span className="font-mono text-[11px] leading-tight text-zinc-400">{proxy.protocol} · {proxy.listen}</span></button>
      <Link className="text-xs font-semibold text-zinc-300 underline underline-offset-3 transition hover:text-zinc-100" to={`/filters?proxy=${encodeURIComponent(proxy.name)}`}>Manage filters · {proxy.filters.length}</Link>
    </div>
  )
}

// ProxyEditorProps defines the controlled proxy form and its mutation states.
interface ProxyEditorProps {
  initial: ProxyDefinition
  isExisting: boolean
  isSaving: boolean
  isDeleting: boolean
  onSave: (definition: ProxyDefinition) => Promise<void>
  onDelete?: () => void
  filters: FilterView[]
  filtersUnavailable: boolean
  assignments: Map<string, string[]>
}

// ProxyEditor validates and submits a proxy definition together with filter choices.
function ProxyEditor({ initial, isExisting, isSaving, isDeleting, onSave, onDelete, filters, filtersUnavailable, assignments }: ProxyEditorProps) {
  const [draft, setDraft] = useState(initial)
  const [validationError, setValidationError] = useState<string | undefined>()
  const [selectedFilter, setSelectedFilter] = useState('')

  // update changes one draft field and clears the previous validation message.
  function update<K extends keyof ProxyDefinition>(key: K, value: ProxyDefinition[K]) {
    setDraft((current) => ({ ...current, [key]: value }))
    setValidationError(undefined)
  }

  function addFilter() {
    const filter = filters.find((current) => current.name === selectedFilter)
    if (!filter || draft.filters.includes(filter.name) || !filter.protocols.includes(draft.protocol)) return
    update('filters', [...draft.filters, filter.name])
    setSelectedFilter('')
  }

  function removeFilter(name: string) {
    update('filters', draft.filters.filter((current) => current !== name))
  }

  // submit performs client-side shape validation before calling the API mutation.
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const parsed = proxyDefinitionSchema.safeParse(draft)
    if (!parsed.success) {
      setValidationError('Name, listen address, and upstream are required.')
      return
    }
    await onSave(parsed.data)
  }

  return (
    <form className="grid gap-6" onSubmit={(event) => void submit(event)}>
      <div className="grid grid-cols-2 gap-4 max-sm:grid-cols-1">
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Name<input className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10 disabled:cursor-not-allowed disabled:opacity-60" value={draft.name} onChange={(event) => update('name', event.target.value)} disabled={isExisting} required /></label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Protocol<select className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={draft.protocol} onChange={(event) => update('protocol', event.target.value as ProxyDefinition['protocol'])}><option value="tcp">TCP</option><option value="http">HTTP</option></select></label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Listen<input className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={draft.listen} onChange={(event) => update('listen', event.target.value)} placeholder="127.0.0.1:31337" required /></label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Upstream<input className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={draft.upstream} onChange={(event) => update('upstream', event.target.value)} placeholder="127.0.0.1:31338" required /></label>
      </div>
      <label className="flex items-center gap-2 text-sm text-zinc-100"><input className="accent-zinc-200" type="checkbox" checked={draft.active} onChange={(event) => update('active', event.target.checked)} /> Start active</label>
      <fieldset className="grid gap-3 border-t border-zinc-700 pt-5">
        <legend className="px-0 text-sm font-semibold text-zinc-100">Filters</legend>
        <p className="m-0 text-xs leading-relaxed text-zinc-400">Choose from every available filter. The dropdown shows where each filter is already used; a filter can be shared by multiple proxies.</p>
        {filtersUnavailable && <p className="m-0 text-xs text-zinc-400" role="alert">Available filters could not be loaded. Existing assignments are unchanged.</p>}
        {!filtersUnavailable && <div className="flex gap-2 max-sm:flex-col">
          <select aria-label="Available filters" className="h-10 min-w-0 flex-1 rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={selectedFilter} onChange={(event) => setSelectedFilter(event.target.value)}>
            <option value="">Select a filter to attach</option>
            {filters.map((filter) => {
              const attached = draft.filters.includes(filter.name)
              const compatible = filter.protocols.includes(draft.protocol)
              return <option key={filter.name} value={filter.name} disabled={attached || !compatible}>{filterOptionLabel(filter, assignments.get(filter.name) ?? [], attached, compatible, draft.protocol)}</option>
            })}
          </select>
          <button type="button" className="min-h-10 shrink-0 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900 disabled:cursor-not-allowed disabled:opacity-50" onClick={addFilter} disabled={!selectedFilter}>Attach filter</button>
        </div>}
        {draft.filters.length === 0
          ? <p className="m-0 text-sm text-zinc-400">No filters attached.</p>
          : <ul className="m-0 grid list-none gap-2 p-0">{draft.filters.map((name) => <li key={name} className="flex items-center justify-between gap-3 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2"><span className="min-w-0 break-words font-mono text-xs text-zinc-200">{name}</span><button type="button" className="shrink-0 cursor-pointer bg-transparent p-0 text-xs font-semibold text-zinc-400 underline underline-offset-3 transition hover:text-zinc-100" onClick={() => removeFilter(name)}>Detach</button></li>)}</ul>}
      </fieldset>
      {validationError && <p className="m-0 text-sm text-zinc-200" role="alert">{validationError}</p>}
      <div className="flex items-center gap-2 border-t border-zinc-700 pt-1">
        <button type="submit" className="min-h-9 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900 disabled:cursor-wait disabled:opacity-60" disabled={isSaving}>{isSaving ? 'Saving…' : 'Save proxy'}</button>
        {onDelete && <button type="button" className="min-h-9 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-400 transition hover:border-zinc-100 hover:text-zinc-100 disabled:cursor-wait disabled:opacity-60 ml-auto" onClick={onDelete} disabled={isDeleting}>{isDeleting ? 'Removing…' : 'Remove proxy'}</button>}
      </div>
    </form>
  )
}

// toDefinition removes runtime-only proxy state before populating the editor.
function toDefinition(proxy: ProxyView): ProxyDefinition {
  return proxyDefinitionSchema.parse(proxy)
}

function filterAssignments(proxies: ProxyView[]): Map<string, string[]> {
  const assignments = new Map<string, string[]>()
  for (const proxy of proxies) {
    for (const name of proxy.filters) {
      const current = assignments.get(name) ?? []
      current.push(proxy.name)
      assignments.set(name, current)
    }
  }
  return assignments
}

function filterOptionLabel(filter: FilterView, assignedProxies: string[], attached: boolean, compatible: boolean, protocol: ProxyDefinition['protocol']): string {
  const assignment = assignedProxies.length === 0 ? 'unassigned' : `used by ${assignedProxies.join(', ')}`
  if (attached) return `${filter.name} — attached to this proxy; ${assignment}`
  if (!compatible) return `${filter.name} — incompatible with ${protocol.toUpperCase()}; ${assignment}`
  return `${filter.name} — ${assignment}`
}
