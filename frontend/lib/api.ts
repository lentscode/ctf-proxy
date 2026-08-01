import { z } from "zod";
import { authenticatedFetch } from "./auth";

// APIError carries a user-safe message and optional HTTP status for callers.
export class APIError extends Error {
  status?: number;

  // constructor preserves the API status while presenting a regular Error shape.
  constructor(message: string, status?: number) {
    super(message);
    this.name = "APIError";
    this.status = status;
  }
}

// Response schemas form the browser's trust boundary: even the authenticated
// local API is parsed before its data can affect dashboard state.
const proxyStateSchema = z.enum(["running", "inactive", "failed"]);
const proxyViewSchema = z.object({
  name: z.string(),
  active: z.boolean(),
  protocol: z.enum(["tcp", "http"]),
  listen: z.string(),
  upstream: z.string(),
  filters: z.array(z.string()),
  state: proxyStateSchema,
});

export const proxyDefinitionSchema = z.object({
  name: z.string().min(1),
  active: z.boolean(),
  protocol: z.enum(["tcp", "http"]),
  listen: z.string().min(1),
  upstream: z.string().min(1),
  filters: z.array(z.string()),
});

const filterViewSchema = z.object({
  name: z.string(),
  active: z.boolean(),
  source: z.string(),
  editable: z.boolean(),
  protocols: z.array(z.string()),
  directions: z.array(z.string()),
  needs_http_body: z.boolean(),
  assigned_proxies: z.array(z.string()),
});

const managedFilterViewSchema = filterViewSchema.extend({
  yaml: z.string(),
});

const eventSchema = z.object({
  id: z.number().int().nonnegative(),
  time: z.string().datetime({ offset: true }),
  level: z.enum(["warn", "error"]),
  component: z.enum(["filter", "proxy", "control"]),
  kind: z.string(),
  proxy: z.string().optional(),
  filter: z.string().optional(),
  protocol: z.string().optional(),
  direction: z.string().optional(),
  message: z.string(),
});

const healthSchema = z.object({ status: z.literal("ok") });
const proxiesSchema = z.object({ proxies: z.array(proxyViewSchema) });
const eventsSchema = z.object({ events: z.array(eventSchema) });
const filtersSchema = z.object({ filters: z.array(filterViewSchema) });
const composeCandidateSchema = z.object({
  id: z.string(),
  project: z.string(),
  compose_file: z.string(),
  service: z.string(),
  listen: z.string(),
  target: z.string(),
  upstream: z.string(),
  eligible: z.boolean(),
  reason: z.string().optional(),
});
const composeProjectSchema = z.object({
  name: z.string(),
  compose_file: z.string(),
  candidates: z.array(composeCandidateSchema),
});
const composeDiscoverySchema = z.object({
  projects: z.array(composeProjectSchema),
  revision: z.string(),
});
const deploymentSchema = z.object({
  id: z.string(),
  project: z.string(),
  compose_file: z.string(),
  service: z.string(),
  listen: z.string(),
  upstream: z.string(),
  proxy: z.string(),
  protocol: z.enum(["tcp", "http"]),
  state: z.string(),
});
const deploymentsSchema = z.object({ deployments: z.array(deploymentSchema) });
const metricValuesSchema = z.object({
  requests: z.number().optional(),
  responses: z.number().optional(),
  connections_accepted: z.number().optional(),
  connections_active: z.number().optional(),
  client_chunks: z.number().optional(),
  server_chunks: z.number().optional(),
  client_to_upstream_bytes: z.number(),
  upstream_to_client_bytes: z.number(),
  rejections_total: z.number(),
  filter_rejections: z.number(),
  capacity_rejections: z.number(),
  upstream_failures: z.number(),
  rejection_denominator: z.number(),
  rejection_ratio: z.number(),
});
const metricRoundSchema = z.object({
  number: z.number(),
  starts_at: z.string().datetime({ offset: true }),
  ends_at: z.string().datetime({ offset: true }),
  metrics: metricValuesSchema,
});
const metricsSchema = z.object({
  collected_since: z.string().datetime({ offset: true }),
  schedule: z.object({
    competition_start: z.string().datetime({ offset: true }),
    round_duration_seconds: z.number(),
    retention_rounds: z.number(),
  }),
  current_round: metricRoundSchema.nullable(),
  proxies: z.array(
    z.object({
      name: z.string(),
      protocol: z.enum(["tcp", "http"]),
      configured: z.boolean(),
      metrics: metricValuesSchema,
    }),
  ),
});
const metricRoundsSchema = z.object({ rounds: z.array(metricRoundSchema) });

// ProxyView is the server's proxy representation, including runtime state.
export type ProxyView = z.infer<typeof proxyViewSchema>;
// ProxyDefinition is the editable subset accepted by proxy mutations.
export type ProxyDefinition = z.infer<typeof proxyDefinitionSchema>;
// FilterView is the filter metadata presented by the dashboard.
export type FilterView = z.infer<typeof filterViewSchema>;
// ManagedFilterView is the editable API-managed filter source and its assignments.
export type ManagedFilterView = z.infer<typeof managedFilterViewSchema>;
// ObserveEvent is the sanitized event shape accepted from API and SSE responses.
export type ObserveEvent = z.infer<typeof eventSchema>;
export type ScanCandidate = z.infer<typeof composeCandidateSchema>;
export type ScanProject = z.infer<typeof composeProjectSchema>;
export type ManagedDeployment = z.infer<typeof deploymentSchema>;
export type ScanDiscovery = z.infer<typeof composeDiscoverySchema>;
export type MetricValues = z.infer<typeof metricValuesSchema>;
export type MetricRound = z.infer<typeof metricRoundSchema>;
export type MetricsSummary = z.infer<typeof metricsSchema>;
// Compose* aliases preserve the dashboard's operator-facing naming while the
// API module also exposes the more explicit Scan*/Managed* names above.
export type ComposeCandidate = ScanCandidate;
export type ComposeProject = ScanProject;
export type ComposeDeployment = ManagedDeployment;

// isUnauthorized identifies an expired or invalid bearer-token response.
export function isUnauthorized(error: unknown): boolean {
  return error instanceof APIError && error.status === 401;
}

// request performs an authenticated JSON request and validates its response schema.
async function request<T>(
  path: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  let response: Response;
  try {
    const headers = new Headers(init?.headers);
    headers.set("Accept", "application/json");
    response = await authenticatedFetch(path, { ...init, headers });
  } catch {
    throw new APIError("Unable to reach ctf-proxy.");
  }

  if (!response.ok) {
    throw new APIError(
      `ctf-proxy returned ${response.status}`,
      response.status,
    );
  }

  if (response.status === 204) {
    return schema.parse(undefined);
  }

  let body: unknown;
  try {
    body = await response.json();
  } catch {
    throw new APIError("ctf-proxy returned invalid JSON.", response.status);
  }

  const parsed = schema.safeParse(body);
  if (!parsed.success) {
    throw new APIError(
      "ctf-proxy returned an invalid response.",
      response.status,
    );
  }
  return parsed.data;
}

// verifyHealth checks that the control API accepts the current token.
export async function verifyHealth(): Promise<void> {
  await request("/healthz", healthSchema);
}

// getProxies fetches and validates the configured proxy list.
export async function getProxies(): Promise<ProxyView[]> {
  return (await request("/api/v1/proxies", proxiesSchema)).proxies;
}

// getEvents fetches the bounded retained event history.
export async function getEvents(): Promise<ObserveEvent[]> {
  return (await request("/api/v1/events?limit=100", eventsSchema)).events;
}

// getFilters fetches the complete filter library and current proxy assignments.
export async function getFilters(): Promise<FilterView[]> {
  return (await request("/api/v1/filters", filtersSchema)).filters;
}

// getMetrics fetches the current competition round and every configured proxy.
export async function getMetrics(): Promise<MetricsSummary> {
  return request("/api/v1/metrics", metricsSchema);
}
// getMetricRounds fetches retained history for one proxy, with URL-safe names.
export async function getMetricRounds(proxy: string): Promise<MetricRound[]> {
  return (
    await request(
      `/api/v1/metrics/rounds?proxy=${encodeURIComponent(proxy)}`,
      metricRoundsSchema,
    )
  ).rounds;
}

// scanProjects discovers eligible published Compose mappings without editing them.
export async function scanProjects(): Promise<
  z.infer<typeof composeDiscoverySchema>
> {
  return request("/api/v1/scan-and-configure/projects", composeDiscoverySchema);
}
// getManagedDeployments lists takeovers that can be restored by the operator.
export async function getManagedDeployments(): Promise<ManagedDeployment[]> {
  return (
    await request("/api/v1/scan-and-configure/deployments", deploymentsSchema)
  ).deployments;
}
// applyScanConfiguration commits an operator-reviewed set of Compose takeovers.
export async function applyScanConfiguration(
  revision: string,
  selections: Array<{
    id: string;
    protocol: "tcp" | "http";
    scheme?: "http" | "https";
  }>,
): Promise<ManagedDeployment[]> {
  return (
    await request("/api/v1/scan-and-configure/apply", deploymentsSchema, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ revision, selections }),
    })
  ).deployments;
}
// restoreManagedDeployments restores selected deployments, or all when requested.
export async function restoreManagedDeployments(
  ids: string[],
  all = false,
): Promise<ManagedDeployment[]> {
  return (
    await request("/api/v1/scan-and-configure/restore", deploymentsSchema, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ids, all }),
    })
  ).deployments;
}

export const discoverCompose = scanProjects;
export const getComposeDeployments = getManagedDeployments;
export const applyCompose = applyScanConfiguration;
export const restoreCompose = restoreManagedDeployments;

// getManagedFilter loads the editable source and assignments for one managed filter.
export async function getManagedFilter(
  name: string,
): Promise<ManagedFilterView> {
  return request(
    `/api/v1/filters/${encodeURIComponent(name)}`,
    managedFilterViewSchema,
  );
}

// createManagedFilter creates one unassigned API-managed YAML filter.
export async function createManagedFilter(
  yaml: string,
): Promise<ManagedFilterView> {
  return request("/api/v1/filters", managedFilterViewSchema, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ yaml }),
  });
}

// replaceFilterAssignments atomically replaces the proxies using one filter.
export async function replaceFilterAssignments(
  name: string,
  proxies: string[],
): Promise<FilterView> {
  return request(
    `/api/v1/filters/${encodeURIComponent(name)}/assignments`,
    filterViewSchema,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ proxies }),
    },
  );
}

// replaceManagedFilter updates one API-managed YAML filter without renaming it.
export async function replaceManagedFilter(
  name: string,
  yaml: string,
): Promise<ManagedFilterView> {
  return request(
    `/api/v1/filters/${encodeURIComponent(name)}`,
    managedFilterViewSchema,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ yaml }),
    },
  );
}

// deleteManagedFilter deletes an unassigned API-managed YAML filter.
export async function deleteManagedFilter(name: string): Promise<void> {
  await request(`/api/v1/filters/${encodeURIComponent(name)}`, z.undefined(), {
    method: "DELETE",
  });
}

// createProxy creates and validates a new proxy through the control API.
export async function createProxy(
  definition: ProxyDefinition,
): Promise<ProxyView> {
  return request("/api/v1/proxies", proxyViewSchema, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(definition),
  });
}

// replaceProxy replaces the named proxy. definition.name may rename the
// resource identified by name.
export async function replaceProxy(
  name: string,
  definition: ProxyDefinition,
): Promise<ProxyView> {
  return request(
    `/api/v1/proxies/${encodeURIComponent(name)}`,
    proxyViewSchema,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(definition),
    },
  );
}

// deleteProxy removes the named proxy and expects an empty successful response.
export async function deleteProxy(name: string): Promise<void> {
  await request(`/api/v1/proxies/${encodeURIComponent(name)}`, z.undefined(), {
    method: "DELETE",
  });
}

// parseObserveEvent accepts only event payloads matching the runtime schema.
export function parseObserveEvent(value: unknown): ObserveEvent | undefined {
  const parsed = eventSchema.safeParse(value);
  return parsed.success ? parsed.data : undefined;
}
