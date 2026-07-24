import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { ManagedFilterForm } from './ManagedFilterForm'
import { createEmptyDraft, parseManagedFilterYAML, serializeManagedFilterYAML, type ManagedFilterDraft } from './managed-filter-form'
import { createManagedFilter, deleteManagedFilter, getFilters, getManagedFilter, getProxies, isUnauthorized, proxyDefinitionSchema, replaceManagedFilter, replaceProxy, type FilterView, type ManagedFilterView, type ProxyView } from '../lib/api'
import { queryClient } from '../lib/query-client'

interface FiltersPageProps {
  onUnauthorized: () => void
}

type Editor =
  | { mode: 'create', proxy: ProxyView }
  | { mode: 'edit', proxyName: string, filterName: string }
  | undefined

// FiltersPage manages API-managed filters in the context of their assigned proxies.
export function FiltersPage({ onUnauthorized }: FiltersPageProps) {
  const [searchParams] = useSearchParams()
  const [editor, setEditor] = useState<Editor>(undefined)
  const [removalNotice, setRemovalNotice] = useState<string | undefined>()
  const focusedProxy = searchParams.get('proxy') ?? undefined
  const focusedOnce = useRef<string | undefined>(undefined)
  const proxies = useQuery({ queryKey: ['proxies'], queryFn: getProxies })
  const filters = useQuery({ queryKey: ['filters'], queryFn: getFilters })
  const editingName = editor?.mode === 'edit' ? editor.filterName : undefined
  const managed = useQuery({
    queryKey: ['managed-filter', editingName],
    queryFn: () => getManagedFilter(editingName ?? ''),
    enabled: Boolean(editingName),
  })

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['proxies'] }),
      queryClient.invalidateQueries({ queryKey: ['filters'] }),
      queryClient.invalidateQueries({ queryKey: ['managed-filter'] }),
    ])
  }
  const create = useMutation({
    mutationFn: ({ proxyName, draft }: { proxyName: string, draft: ManagedFilterDraft }) => createManagedFilter(proxyName, serializeManagedFilterYAML(draft)),
    onSuccess: async () => { setEditor(undefined); await refresh() },
  })
  const replace = useMutation({
    mutationFn: ({ name, draft }: { name: string, draft: ManagedFilterDraft }) => replaceManagedFilter(name, serializeManagedFilterYAML(draft)),
    onSuccess: async () => { setEditor(undefined); await refresh() },
  })
  const remove = useMutation({
    mutationFn: async ({ proxy, filter }: { proxy: ProxyView, filter: FilterView }) => {
      const definition = proxyDefinitionSchema.parse({ ...proxy, filters: proxy.filters.filter((name) => name !== filter.name) })
      await replaceProxy(proxy.name, definition)
      if (!filter.editable) return { cleanupFailed: false, cleaned: false }
      try {
        const detail = await getManagedFilter(filter.name)
        if (detail.assigned_proxies.length === 0) {
          await deleteManagedFilter(filter.name)
          return { cleanupFailed: false, cleaned: true }
        }
      } catch (error) {
        if (isUnauthorized(error)) throw error
        return { cleanupFailed: true, cleaned: false }
      }
      return { cleanupFailed: false, cleaned: false }
    },
    onSuccess: async (result) => {
      setEditor(undefined)
      setRemovalNotice(result.cleanupFailed ? 'Filter detached, but its unused managed definition could not be deleted.' : result.cleaned ? 'Filter detached and its unused managed definition was deleted.' : 'Filter detached from this proxy.')
      await refresh()
    },
  })

  useEffect(() => {
    const errors = [proxies.error, filters.error, managed.error, create.error, replace.error, remove.error]
    if (errors.some(isUnauthorized)) onUnauthorized()
  }, [create.error, filters.error, managed.error, onUnauthorized, proxies.error, remove.error, replace.error])

  useEffect(() => {
    if (!focusedProxy || focusedOnce.current === focusedProxy || !proxies.data?.some((proxy) => proxy.name === focusedProxy)) return
    const heading = document.getElementById(proxySectionID(focusedProxy))
    if (!heading) return
    focusedOnce.current = focusedProxy
    heading.scrollIntoView({ behavior: 'smooth', block: 'start' })
    heading.focus({ preventScroll: true })
  }, [focusedProxy, proxies.data])

  const filtersByName = useMemo(() => new Map(filters.data?.map((filter) => [filter.name, filter])), [filters.data])
  const mutationError = [create.error, replace.error, remove.error].find((error) => error && !isUnauthorized(error))

  function confirmRemove(proxy: ProxyView, filter: FilterView) {
    if (window.confirm(`Detach filter “${filter.name}” from proxy “${proxy.name}”?`)) {
      setRemovalNotice(undefined)
      remove.mutate({ proxy, filter })
    }
  }

  return (
    <main className="mx-auto w-full max-w-[1440px] px-8 pt-14 pb-8 max-lg:px-6 max-lg:pt-10 max-lg:pb-6 max-sm:px-4 max-sm:pt-8 max-sm:pb-4">
      <header className="flex min-h-26 items-end justify-between gap-4 border-b border-zinc-700 pb-6 max-sm:min-h-0 max-sm:items-start">
        <div><p className="m-0 font-mono text-[11px] leading-none tracking-[0.08em] text-zinc-400 uppercase">Configuration</p><h1 className="mt-1.5 mb-0 text-3xl font-semibold tracking-tight text-zinc-100">Filters</h1></div>
      </header>
      {removalNotice && <p className="m-0 border-b border-zinc-700 py-4 text-sm text-zinc-200" role="status">{removalNotice}</p>}
      {mutationError && <p className="m-0 border-b border-zinc-700 py-4 text-sm text-zinc-200" role="alert">Unable to update this filter. Check the values and try again.</p>}
      {proxies.isLoading && <p className="m-0 py-6 text-sm text-zinc-400">Loading proxy filters…</p>}
      {proxies.isError && !isUnauthorized(proxies.error) && <p className="m-0 py-6 text-sm text-zinc-400">Unable to load proxies.</p>}
      {proxies.data?.length === 0 && <p className="m-0 py-6 text-sm text-zinc-400">No proxies configured.</p>}
      {filters.isError && !isUnauthorized(filters.error) && <p className="m-0 border-b border-zinc-700 py-4 text-sm text-zinc-400">Filter metadata is unavailable. Attached filters can still be detached.</p>}
      <div className="grid divide-y divide-zinc-700">
        {proxies.data?.map((proxy) => {
          const groupFilters = proxy.filters.map((name) => filtersByName.get(name) ?? unavailableFilter(name))
          const currentEditor = editor?.mode === 'create' && editor.proxy.name === proxy.name
            ? <ManagedFilterForm key={`create-${proxy.name}`} initial={createEmptyDraft(proxy.protocol)} isExisting={false} isSaving={create.isPending} saveError={create.error && !isUnauthorized(create.error) ? 'Unable to create this filter. Check the values and try again.' : undefined} onSave={async (draft) => { await create.mutateAsync({ proxyName: proxy.name, draft }) }} onCancel={() => setEditor(undefined)} />
            : editor?.mode === 'edit' && editor.proxyName === proxy.name
              ? <ManagedEditor filterName={editor.filterName} managed={managed} isSaving={replace.isPending} saveError={replace.error && !isUnauthorized(replace.error) ? 'Unable to save this filter. Check the values and try again.' : undefined} onSave={async (draft) => { await replace.mutateAsync({ name: editor.filterName, draft }) }} onCancel={() => setEditor(undefined)} />
              : undefined
          return <section key={proxy.name} className={`scroll-mt-6 ${focusedProxy === proxy.name ? 'bg-zinc-900/45 shadow-[inset_2px_0_0_0_#f4f4f5]' : ''}`} aria-labelledby={proxySectionID(proxy.name)}>
            <header className="flex items-center gap-4 px-5 py-5 max-sm:items-start">
              <div className="min-w-0"><h2 id={proxySectionID(proxy.name)} tabIndex={-1} className="m-0 text-base font-semibold text-zinc-100 outline-none">{proxy.name}</h2><p className="mt-1 mb-0 font-mono text-[11px] text-zinc-400">{proxy.protocol} · {proxy.listen} · {groupFilters.length} {groupFilters.length === 1 ? 'filter' : 'filters'}</p></div>
              <button type="button" className="ml-auto min-h-9 shrink-0 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900" onClick={() => { setRemovalNotice(undefined); setEditor({ mode: 'create', proxy }) }}>Add filter</button>
            </header>
            {groupFilters.length === 0 && <p className="m-0 border-t border-zinc-700 px-5 py-4 text-sm text-zinc-400">No filters attached.</p>}
            {groupFilters.map((filter) => <FilterRow key={filter.name} filter={filter} isRemoving={remove.isPending} onEdit={filter.editable ? () => { setRemovalNotice(undefined); setEditor({ mode: 'edit', proxyName: proxy.name, filterName: filter.name }) } : undefined} onRemove={() => confirmRemove(proxy, filter)} />)}
            {currentEditor}
          </section>
        })}
      </div>
    </main>
  )
}

function ManagedEditor({ filterName, managed, isSaving, saveError, onSave, onCancel }: {
  filterName: string
  managed: UseQueryResult<ManagedFilterView, Error>
  isSaving: boolean
  saveError?: string
  onSave: (draft: ManagedFilterDraft) => Promise<void>
  onCancel: () => void
}) {
  if (managed.isLoading) return <p className="m-0 border-t border-zinc-700 px-5 py-5 text-sm text-zinc-400">Loading filter configuration…</p>
  if (managed.isError) return <div className="grid gap-3 border-t border-zinc-700 px-5 py-5"><p className="m-0 text-sm text-zinc-400">Unable to load this filter configuration.</p><button type="button" className="justify-self-start bg-transparent p-0 text-sm text-zinc-200 underline underline-offset-3" onClick={() => void managed.refetch()}>Retry</button></div>
  if (!managed.data || managed.data.name !== filterName) return null
  let initial: ManagedFilterDraft
  try {
    initial = parseManagedFilterYAML(managed.data.yaml)
  } catch {
    return <p className="m-0 border-t border-zinc-700 px-5 py-5 text-sm text-zinc-400" role="alert">This managed filter cannot be represented by the form.</p>
  }
  return <ManagedFilterForm key={`edit-${filterName}`} initial={initial} isExisting assignedProxies={managed.data.assigned_proxies} isSaving={isSaving} saveError={saveError} onSave={onSave} onCancel={onCancel} />
}

function FilterRow({ filter, isRemoving, onEdit, onRemove }: { filter: FilterView, isRemoving: boolean, onEdit?: () => void, onRemove: () => void }) {
  const metadata = [filter.active ? 'active' : 'inactive', ...filter.protocols, ...filter.directions, filter.needs_http_body ? 'HTTP body' : undefined].filter(Boolean)
  return <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 border-t border-zinc-700 px-5 py-4 max-sm:items-start" role="group" aria-label={`Filter ${filter.name}`}>
    <div className="min-w-0"><p className="m-0 text-sm text-zinc-100">{filter.name}</p><p className="mt-1 mb-0 break-words font-mono text-[11px] leading-tight text-zinc-400">{filter.source} · {metadata.join(' · ') || 'metadata unavailable'}</p></div>
    <div className="flex items-center gap-2">{onEdit && <button type="button" className="min-h-8 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-2.5 text-xs font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900" onClick={onEdit}>Edit</button>}<button type="button" className="min-h-8 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-2.5 text-xs font-semibold text-zinc-400 transition hover:border-zinc-100 hover:text-zinc-100 disabled:cursor-wait disabled:opacity-60" onClick={onRemove} disabled={isRemoving}>{isRemoving ? 'Removing…' : 'Remove'}</button></div>
  </div>
}

function unavailableFilter(name: string): FilterView {
  return { name, active: false, source: 'unavailable', editable: false, protocols: [], directions: [], needs_http_body: false }
}

function proxySectionID(name: string): string {
  return `proxy-filter-group-${encodeURIComponent(name)}`
}
