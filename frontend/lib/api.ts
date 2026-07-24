import { z } from 'zod'
import { authenticatedFetch } from './auth'

// APIError carries a user-safe message and optional HTTP status for callers.
export class APIError extends Error {
  status?: number

  // constructor preserves the API status while presenting a regular Error shape.
  constructor(message: string, status?: number) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

const proxyStateSchema = z.enum(['running', 'inactive', 'failed'])
const proxyViewSchema = z.object({
  name: z.string(),
  active: z.boolean(),
  protocol: z.enum(['tcp', 'http']),
  listen: z.string(),
  upstream: z.string(),
  filters: z.array(z.string()),
  state: proxyStateSchema,
})

export const proxyDefinitionSchema = z.object({
  name: z.string().min(1),
  active: z.boolean(),
  protocol: z.enum(['tcp', 'http']),
  listen: z.string().min(1),
  upstream: z.string().min(1),
  filters: z.array(z.string()),
})

const filterViewSchema = z.object({
  name: z.string(),
  source: z.string(),
  editable: z.boolean(),
  protocols: z.array(z.string()),
  directions: z.array(z.string()),
  needs_http_body: z.boolean(),
})

const managedFilterViewSchema = filterViewSchema.extend({
  yaml: z.string(),
  assigned_proxies: z.array(z.string()),
})

const eventSchema = z.object({
  id: z.number().int().nonnegative(),
  time: z.string().datetime({ offset: true }),
  level: z.enum(['warn', 'error']),
  component: z.enum(['filter', 'proxy', 'control']),
  kind: z.string(),
  proxy: z.string().optional(),
  filter: z.string().optional(),
  protocol: z.string().optional(),
  direction: z.string().optional(),
  message: z.string(),
})

const healthSchema = z.object({ status: z.literal('ok') })
const proxiesSchema = z.object({ proxies: z.array(proxyViewSchema) })
const eventsSchema = z.object({ events: z.array(eventSchema) })
const filtersSchema = z.object({ filters: z.array(filterViewSchema) })

// ProxyView is the server's proxy representation, including runtime state.
export type ProxyView = z.infer<typeof proxyViewSchema>
// ProxyDefinition is the editable subset accepted by proxy mutations.
export type ProxyDefinition = z.infer<typeof proxyDefinitionSchema>
// FilterView is the filter metadata presented by the dashboard.
export type FilterView = z.infer<typeof filterViewSchema>
// ManagedFilterView is the editable API-managed filter source and its assignments.
export type ManagedFilterView = z.infer<typeof managedFilterViewSchema>
// ObserveEvent is the sanitized event shape accepted from API and SSE responses.
export type ObserveEvent = z.infer<typeof eventSchema>

// isUnauthorized identifies an expired or invalid bearer-token response.
export function isUnauthorized(error: unknown): boolean {
  return error instanceof APIError && error.status === 401
}

// request performs an authenticated JSON request and validates its response schema.
async function request<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    const headers = new Headers(init?.headers)
    headers.set('Accept', 'application/json')
    response = await authenticatedFetch(path, { ...init, headers })
  } catch {
    throw new APIError('Unable to reach ctf-proxy.')
  }

  if (!response.ok) {
    throw new APIError(`ctf-proxy returned ${response.status}`, response.status)
  }

  if (response.status === 204) {
    return schema.parse(undefined)
  }

  let body: unknown
  try {
    body = await response.json()
  } catch {
    throw new APIError('ctf-proxy returned invalid JSON.', response.status)
  }

  const parsed = schema.safeParse(body)
  if (!parsed.success) {
    throw new APIError('ctf-proxy returned an invalid response.', response.status)
  }
  return parsed.data
}

// verifyHealth checks that the control API accepts the current token.
export async function verifyHealth(): Promise<void> {
  await request('/healthz', healthSchema)
}

// getProxies fetches and validates the configured proxy list.
export async function getProxies(): Promise<ProxyView[]> {
  return (await request('/api/v1/proxies', proxiesSchema)).proxies
}

// getEvents fetches the bounded retained event history.
export async function getEvents(): Promise<ObserveEvent[]> {
  return (await request('/api/v1/events?limit=100', eventsSchema)).events
}

// getFilters fetches metadata for filters available to proxy definitions.
export async function getFilters(): Promise<FilterView[]> {
  return (await request('/api/v1/filters', filtersSchema)).filters
}

// getManagedFilter loads the editable source and assignments for one managed filter.
export async function getManagedFilter(name: string): Promise<ManagedFilterView> {
  return request(`/api/v1/filters/${encodeURIComponent(name)}`, managedFilterViewSchema)
}

// createManagedFilter creates and attaches one API-managed YAML filter to a proxy.
export async function createManagedFilter(proxyName: string, yaml: string): Promise<ManagedFilterView> {
  return request(`/api/v1/proxies/${encodeURIComponent(proxyName)}/filters`, managedFilterViewSchema, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yaml }),
  })
}

// replaceManagedFilter updates one API-managed YAML filter without renaming it.
export async function replaceManagedFilter(name: string, yaml: string): Promise<ManagedFilterView> {
  return request(`/api/v1/filters/${encodeURIComponent(name)}`, managedFilterViewSchema, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yaml }),
  })
}

// deleteManagedFilter deletes an unassigned API-managed YAML filter.
export async function deleteManagedFilter(name: string): Promise<void> {
  await request(`/api/v1/filters/${encodeURIComponent(name)}`, z.undefined(), { method: 'DELETE' })
}

// createProxy creates and validates a new proxy through the control API.
export async function createProxy(definition: ProxyDefinition): Promise<ProxyView> {
  return request('/api/v1/proxies', proxyViewSchema, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(definition),
  })
}

// replaceProxy replaces the named proxy while preserving its stable name.
export async function replaceProxy(name: string, definition: ProxyDefinition): Promise<ProxyView> {
  return request(`/api/v1/proxies/${encodeURIComponent(name)}`, proxyViewSchema, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(definition),
  })
}

// deleteProxy removes the named proxy and expects an empty successful response.
export async function deleteProxy(name: string): Promise<void> {
  await request(`/api/v1/proxies/${encodeURIComponent(name)}`, z.undefined(), { method: 'DELETE' })
}

// parseObserveEvent accepts only event payloads matching the runtime schema.
export function parseObserveEvent(value: unknown): ObserveEvent | undefined {
  const parsed = eventSchema.safeParse(value)
  return parsed.success ? parsed.data : undefined
}
