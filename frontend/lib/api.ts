import { z } from 'zod'
import { authenticatedFetch } from './auth'

export class APIError extends Error {
  status?: number

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
  protocols: z.array(z.string()),
  directions: z.array(z.string()),
  needs_http_body: z.boolean(),
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

export type ProxyView = z.infer<typeof proxyViewSchema>
export type ProxyDefinition = z.infer<typeof proxyDefinitionSchema>
export type FilterView = z.infer<typeof filterViewSchema>
export type ObserveEvent = z.infer<typeof eventSchema>

export function isUnauthorized(error: unknown): boolean {
  return error instanceof APIError && error.status === 401
}

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

export async function verifyHealth(): Promise<void> {
  await request('/healthz', healthSchema)
}

export async function getProxies(): Promise<ProxyView[]> {
  return (await request('/api/v1/proxies', proxiesSchema)).proxies
}

export async function getEvents(): Promise<ObserveEvent[]> {
  return (await request('/api/v1/events?limit=100', eventsSchema)).events
}

export async function getFilters(): Promise<FilterView[]> {
  return (await request('/api/v1/filters', filtersSchema)).filters
}

export async function createProxy(definition: ProxyDefinition): Promise<ProxyView> {
  return request('/api/v1/proxies', proxyViewSchema, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(definition),
  })
}

export async function replaceProxy(name: string, definition: ProxyDefinition): Promise<ProxyView> {
  return request(`/api/v1/proxies/${encodeURIComponent(name)}`, proxyViewSchema, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(definition),
  })
}

export async function deleteProxy(name: string): Promise<void> {
  await request(`/api/v1/proxies/${encodeURIComponent(name)}`, z.undefined(), { method: 'DELETE' })
}

export function parseObserveEvent(value: unknown): ObserveEvent | undefined {
  const parsed = eventSchema.safeParse(value)
  return parsed.success ? parsed.data : undefined
}
