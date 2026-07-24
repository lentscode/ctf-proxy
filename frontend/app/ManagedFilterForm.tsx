import { useState } from 'react'
import type { FormEvent } from 'react'
import { availableFields, filterDirectionSchema, filterProtocolSchema, labelForField, managedFilterDraftSchema, matchOperatorSchema, type FilterDirection, type FilterProtocol, type ManagedFilterDraft, type MatchField } from './managed-filter-form'

interface ManagedFilterFormProps {
  initial: ManagedFilterDraft
  isExisting: boolean
  assignedProxies?: string[]
  isSaving: boolean
  saveError?: string
  onSave: (draft: ManagedFilterDraft) => Promise<void>
  onCancel: () => void
}

const operatorLabels: Record<(typeof matchOperatorSchema._output), string> = {
  exact: 'Exact', contains: 'Contains', not_contains: 'Does not contain', prefix: 'Prefix', suffix: 'Suffix', regex: 'Regular expression',
}

// ManagedFilterForm edits the supported single-rule managed-filter model without exposing YAML.
export function ManagedFilterForm({ initial, isExisting, assignedProxies = [], isSaving, saveError, onSave, onCancel }: ManagedFilterFormProps) {
  const [draft, setDraft] = useState(initial)
  const [validationError, setValidationError] = useState<string | undefined>()

  function updateProtocol(protocol: FilterProtocol) {
    setDraft((current) => normalizeDraft({ ...current, protocol }))
    setValidationError(undefined)
  }

  function updateDirection(direction: FilterDirection) {
    setDraft((current) => normalizeDraft({ ...current, direction }))
    setValidationError(undefined)
  }

  function updateCondition(index: number, update: Partial<ManagedFilterDraft['conditions'][number]>) {
    setDraft((current) => ({
      ...current,
      conditions: current.conditions.map((condition, currentIndex) => currentIndex === index ? { ...condition, ...update } : condition),
    }))
    setValidationError(undefined)
  }

  function addCondition() {
    setDraft((current) => ({
      ...current,
      conditions: [...current.conditions, { field: availableFields(current.protocol, current.direction)[0], header: '', operator: 'contains', value: '' }],
    }))
  }

  function removeCondition(index: number) {
    setDraft((current) => ({ ...current, conditions: current.conditions.filter((_, currentIndex) => currentIndex !== index) }))
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const parsed = managedFilterDraftSchema.safeParse(draft)
    if (!parsed.success) {
      setValidationError(parsed.error.issues[0]?.message ?? 'Check the filter configuration and try again.')
      return
    }
    await onSave(parsed.data)
  }

  return (
    <form className="grid gap-5 border-t border-zinc-700 px-5 py-5" onSubmit={(event) => void submit(event)}>
      <div className="grid gap-1">
        <h3 className="m-0 text-sm font-semibold text-zinc-100">{isExisting ? `Edit ${initial.name}` : 'Add managed filter'}</h3>
        <p className="m-0 text-xs leading-relaxed text-zinc-400">This rule rejects traffic only when every condition below matches.</p>
      </div>
      {assignedProxies.length > 1 && <p className="m-0 border-l-2 border-zinc-300 bg-zinc-900 px-3 py-2 text-xs leading-relaxed text-zinc-200" role="note">This filter is shared by {assignedProxies.length} proxies. Saving updates and restarts every assigned proxy: {assignedProxies.join(', ')}.</p>}
      <div className="grid grid-cols-3 gap-4 max-sm:grid-cols-1">
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Filter name
          <input className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10 disabled:cursor-not-allowed disabled:opacity-60" value={draft.name} onChange={(event) => { setDraft((current) => ({ ...current, name: event.target.value })); setValidationError(undefined) }} disabled={isExisting} required />
        </label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Protocol
          <select className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={draft.protocol} onChange={(event) => updateProtocol(filterProtocolSchema.parse(event.target.value))}>
            <option value="tcp">TCP</option><option value="http">HTTP</option>
          </select>
        </label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Direction
          <select className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={draft.direction} onChange={(event) => updateDirection(filterDirectionSchema.parse(event.target.value))}>
            <option value="request">Request</option><option value="response">Response</option>
          </select>
        </label>
      </div>
      <fieldset className="grid gap-3 border-0 p-0">
        <legend className="text-xs font-semibold text-zinc-400">All-match conditions</legend>
        {draft.conditions.map((condition, index) => <ConditionEditor key={index} condition={condition} index={index} protocol={draft.protocol} direction={draft.direction} canRemove={draft.conditions.length > 1} onUpdate={(update) => updateCondition(index, update)} onRemove={() => removeCondition(index)} />)}
        <button type="button" className="justify-self-start cursor-pointer bg-transparent p-0 text-xs font-semibold text-zinc-200 underline underline-offset-3" onClick={addCondition}>Add condition</button>
      </fieldset>
      {(validationError || saveError) && <p className="m-0 text-sm text-zinc-200" role="alert">{validationError ?? saveError}</p>}
      <div className="flex items-center gap-2 border-t border-zinc-700 pt-4">
        <button type="submit" className="min-h-9 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900 disabled:cursor-wait disabled:opacity-60" disabled={isSaving}>{isSaving ? 'Saving…' : isExisting ? 'Save filter' : 'Create filter'}</button>
        <button type="button" className="min-h-9 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-400 transition hover:border-zinc-100 hover:text-zinc-100 disabled:cursor-wait disabled:opacity-60" onClick={onCancel} disabled={isSaving}>Cancel</button>
      </div>
    </form>
  )
}

function ConditionEditor({ condition, index, protocol, direction, canRemove, onUpdate, onRemove }: {
  condition: ManagedFilterDraft['conditions'][number]
  index: number
  protocol: FilterProtocol
  direction: FilterDirection
  canRemove: boolean
  onUpdate: (update: Partial<ManagedFilterDraft['conditions'][number]>) => void
  onRemove: () => void
}) {
  const fields = availableFields(protocol, direction)
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] items-end gap-3 border border-zinc-700 p-3 max-lg:grid-cols-2 max-sm:grid-cols-1">
      <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Field
        <select aria-label={`Condition ${index + 1} field`} className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={condition.field} onChange={(event) => {
          const field = event.target.value as MatchField
          onUpdate({ field, header: field === 'http.header' ? condition.header : '' })
        }}>
          {fields.map((field) => <option key={field} value={field}>{labelForField(field)}</option>)}
        </select>
      </label>
      {condition.field === 'http.header' && <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Header name
        <input aria-label={`Condition ${index + 1} header name`} className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={condition.header} onChange={(event) => onUpdate({ header: event.target.value })} required />
      </label>}
      <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Operator
        <select aria-label={`Condition ${index + 1} operator`} className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={condition.operator} onChange={(event) => onUpdate({ operator: matchOperatorSchema.parse(event.target.value) })}>
          {matchOperatorSchema.options.map((operator) => <option key={operator} value={operator}>{operatorLabels[operator]}</option>)}
        </select>
      </label>
      <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">Match value
        <input aria-label={`Condition ${index + 1} match value`} className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10" value={condition.value} onChange={(event) => onUpdate({ value: event.target.value })} />
      </label>
      <button type="button" className="h-10 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-xs font-semibold text-zinc-400 transition hover:border-zinc-100 hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-50 max-lg:col-span-2 max-sm:col-span-1" onClick={onRemove} disabled={!canRemove}>Remove</button>
      {condition.operator === 'regex' && <p className="col-span-full m-0 text-xs leading-relaxed text-zinc-400">Regular expressions use RE2 syntax, so look-arounds such as <code>(?!checker)</code> are not supported. Choose “Does not contain” to reject a header without <code>checker</code>.</p>}
    </div>
  )
}

function normalizeDraft(draft: ManagedFilterDraft): ManagedFilterDraft {
  const fields = availableFields(draft.protocol, draft.direction)
  return {
    ...draft,
    conditions: draft.conditions.map((condition) => {
      const field = fields.includes(condition.field) ? condition.field : fields[0]
      return { ...condition, field, header: field === 'http.header' ? condition.header : '' }
    }),
  }
}
