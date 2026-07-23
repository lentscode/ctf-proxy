import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { createProxy, deleteProxy, getFilters, getProxies, isUnauthorized, proxyDefinitionSchema, replaceProxy, type FilterView, type ProxyDefinition, type ProxyView } from '../lib/api'
import { queryClient } from '../lib/query-client'

interface ProxiesPageProps {
  onUnauthorized: () => void
}

const emptyProxy: ProxyDefinition = {
  name: '', active: true, protocol: 'tcp', listen: '', upstream: '', filters: [],
}

export function ProxiesPage({ onUnauthorized }: ProxiesPageProps) {
  const [selectedName, setSelectedName] = useState<string | undefined>()
  const proxies = useQuery({ queryKey: ['proxies'], queryFn: getProxies })
  const filters = useQuery({ queryKey: ['filters'], queryFn: getFilters })
  const selected = proxies.data?.find((proxy) => proxy.name === selectedName)

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['proxies'] })
  }
  const create = useMutation({ mutationFn: createProxy, onSuccess: refresh })
  const replace = useMutation({ mutationFn: ({ name, definition }: { name: string, definition: ProxyDefinition }) => replaceProxy(name, definition), onSuccess: refresh })
  const remove = useMutation({ mutationFn: deleteProxy, onSuccess: async () => { setSelectedName(undefined); await refresh() } })

  useEffect(() => {
    if (isUnauthorized(proxies.error) || isUnauthorized(filters.error) || isUnauthorized(create.error) || isUnauthorized(replace.error) || isUnauthorized(remove.error)) {
      onUnauthorized()
    }
  }, [create.error, filters.error, onUnauthorized, proxies.error, remove.error, replace.error])

  async function save(definition: ProxyDefinition) {
    if (selected) {
      await replace.mutateAsync({ name: selected.name, definition })
      setSelectedName(selected.name)
    } else {
      const created = await create.mutateAsync(definition)
      setSelectedName(created.name)
    }
  }

  function confirmDelete() {
    if (selected && window.confirm(`Remove proxy “${selected.name}”?`)) {
      remove.mutate(selected.name)
    }
  }

  const mutationError = [create.error, replace.error, remove.error].find(Boolean)
  return (
    <main className="proxies-page">
      <header className="page-header">
        <div><p className="page-kicker">Configuration</p><h1>Proxies</h1></div>
        <button type="button" className="outline-button" onClick={() => setSelectedName(undefined)}>Add proxy</button>
      </header>
      <div className="proxies-workspace">
        <section className="proxy-directory" aria-labelledby="configured-proxies">
          <h2 id="configured-proxies">Configured proxies</h2>
          {proxies.isLoading && <p className="section-note">Loading proxies…</p>}
          {proxies.isError && !isUnauthorized(proxies.error) && <p className="section-note">Unable to load proxies.</p>}
          {proxies.data?.length === 0 && <p className="section-note">No proxies configured.</p>}
          <div className="directory-list">
            {proxies.data?.map((proxy) => <ProxyDirectoryItem key={proxy.name} proxy={proxy} selected={selectedName === proxy.name} onSelect={() => setSelectedName(proxy.name)} />)}
          </div>
        </section>
        <section className="proxy-editor" aria-labelledby="proxy-editor-heading">
          <h2 id="proxy-editor-heading">{selected ? `Edit ${selected.name}` : 'Add proxy'}</h2>
          {mutationError && !isUnauthorized(mutationError) && <p className="form-error">Unable to save this proxy. Check the values and try again.</p>}
          <ProxyEditor
            key={selected?.name ?? 'new'}
            initial={selected ? toDefinition(selected) : emptyProxy}
            isExisting={Boolean(selected)}
            filters={filters.data ?? []}
            isSaving={create.isPending || replace.isPending}
            onSave={save}
            onDelete={selected ? confirmDelete : undefined}
            isDeleting={remove.isPending}
          />
        </section>
      </div>
    </main>
  )
}

function ProxyDirectoryItem({ proxy, selected, onSelect }: { proxy: ProxyView, selected: boolean, onSelect: () => void }) {
  return (
    <button type="button" className={`directory-item ${selected ? 'is-selected' : ''}`} onClick={onSelect}>
      <span>{proxy.name}</span><span className="directory-meta">{proxy.protocol} · {proxy.listen}</span>
    </button>
  )
}

interface ProxyEditorProps {
  initial: ProxyDefinition
  isExisting: boolean
  filters: FilterView[]
  isSaving: boolean
  isDeleting: boolean
  onSave: (definition: ProxyDefinition) => Promise<void>
  onDelete?: () => void
}

function ProxyEditor({ initial, isExisting, filters, isSaving, isDeleting, onSave, onDelete }: ProxyEditorProps) {
  const [draft, setDraft] = useState(initial)
  const [validationError, setValidationError] = useState<string | undefined>()

  function update<K extends keyof ProxyDefinition>(key: K, value: ProxyDefinition[K]) {
    setDraft((current) => ({ ...current, [key]: value }))
    setValidationError(undefined)
  }

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
    <form className="proxy-form" onSubmit={(event) => void submit(event)}>
      <div className="form-grid">
        <label>Name<input value={draft.name} onChange={(event) => update('name', event.target.value)} disabled={isExisting} required /></label>
        <label>Protocol<select value={draft.protocol} onChange={(event) => update('protocol', event.target.value as ProxyDefinition['protocol'])}><option value="tcp">TCP</option><option value="http">HTTP</option></select></label>
        <label>Listen<input className="mono-input" value={draft.listen} onChange={(event) => update('listen', event.target.value)} placeholder="127.0.0.1:31337" required /></label>
        <label>Upstream<input className="mono-input" value={draft.upstream} onChange={(event) => update('upstream', event.target.value)} placeholder="127.0.0.1:31338" required /></label>
      </div>
      <label className="checkbox-field"><input type="checkbox" checked={draft.active} onChange={(event) => update('active', event.target.checked)} /> Start active</label>
      <fieldset className="filter-fieldset">
        <legend>Filters</legend>
        {filters.length === 0 && <p className="section-note">No filters are available.</p>}
        {filters.map((filter) => {
          const checked = draft.filters.includes(filter.name)
          return <label key={filter.name} className="filter-option"><input type="checkbox" checked={checked} onChange={() => update('filters', checked ? draft.filters.filter((name) => name !== filter.name) : [...draft.filters, filter.name])} /><span>{filter.name}</span><small>{filter.protocols.join(', ')}</small></label>
        })}
      </fieldset>
      {validationError && <p className="form-error" role="alert">{validationError}</p>}
      <div className="form-actions">
        <button type="submit" className="outline-button" disabled={isSaving}>{isSaving ? 'Saving…' : 'Save proxy'}</button>
        {onDelete && <button type="button" className="danger-button" onClick={onDelete} disabled={isDeleting}>{isDeleting ? 'Removing…' : 'Remove proxy'}</button>}
      </div>
    </form>
  )
}

function toDefinition(proxy: ProxyView): ProxyDefinition {
  return proxyDefinitionSchema.parse(proxy)
}
