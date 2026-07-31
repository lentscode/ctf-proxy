import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  APIError,
  getMetricRounds,
  getMetrics,
  isUnauthorized,
  type MetricRound,
  type MetricValues,
} from "../lib/api";

interface Props {
  onUnauthorized: () => void;
}

function formatBytes(value: number) {
  const units = ["B", "KiB", "MiB", "GiB"];
  let n = value;
  let unit = 0;
  while (n >= 1024 && unit < units.length - 1) {
    n /= 1024;
    unit++;
  }
  return `${n.toFixed(unit ? 1 : 0)} ${units[unit]}`;
}
function units(protocol: "tcp" | "http", values: MetricValues) {
  return protocol === "http"
    ? `${values.requests ?? 0} requests / ${values.responses ?? 0} responses`
    : `${values.connections_accepted ?? 0} connections · ${values.client_chunks ?? 0}/${values.server_chunks ?? 0} chunks`;
}

// MetricsPanel presents aggregate traffic without retaining or rendering traffic data.
export function MetricsPanel({ onUnauthorized }: Props) {
  const summary = useQuery({
    queryKey: ["metrics"],
    queryFn: getMetrics,
    refetchInterval: 5_000,
  });
  const [selected, setSelected] = useState<string>();
  const history = useQuery({
    queryKey: ["metrics", selected],
    queryFn: () => getMetricRounds(selected!),
    enabled: !!selected,
    refetchInterval: 5_000,
  });
  useEffect(() => {
    if (isUnauthorized(summary.error) || isUnauthorized(history.error))
      onUnauthorized();
  }, [summary.error, history.error, onUnauthorized]);
  if (summary.isLoading)
    return (
      <section
        className="mb-8 h-48 animate-pulse border border-zinc-800 bg-zinc-900"
        aria-label="Loading traffic metrics"
      />
    );
  if (summary.error instanceof APIError && summary.error.status === 503)
    return (
      <section className="mb-8 border border-zinc-800 bg-zinc-900 p-5 text-sm text-zinc-400">
        Traffic metrics are disabled. Add a global{" "}
        <code className="text-zinc-200">metrics</code> section to the YAML
        configuration.
      </section>
    );
  if (summary.isError || !summary.data)
    return (
      <section className="mb-8 border border-red-900 bg-zinc-900 p-5 text-sm text-red-200">
        Unable to load traffic metrics.
      </section>
    );
  const data = summary.data;
  return (
    <section
      className="mb-8 overflow-hidden border border-zinc-700 bg-zinc-950"
      aria-labelledby="metrics-heading"
    >
      <header className="flex flex-wrap items-baseline justify-between gap-2 border-b border-zinc-700 px-5 py-4">
        <div>
          <h1 id="metrics-heading" className="font-semibold text-zinc-100">
            Traffic metrics
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            Round {data.current_round?.number ?? "not started"} ·{" "}
            {data.schedule.round_duration_seconds}s · starts{" "}
            {new Date(data.schedule.competition_start).toLocaleString()}
          </p>
        </div>
        <p className="text-xs text-zinc-500">
          Collected since {new Date(data.collected_since).toLocaleTimeString()}
        </p>
      </header>
      <div className="overflow-auto">
        <table className="w-full min-w-225 text-left text-xs">
          <thead className="bg-zinc-900 text-zinc-400">
            <tr>
              <th className="p-3">Service</th>
              <th className="p-3">Units</th>
              <th className="p-3">Forwarded bytes</th>
              <th className="p-3">Rejected</th>
              <th className="p-3">Failures</th>
            </tr>
          </thead>
          <tbody>
            {data.proxies.map((proxy) => (
              <tr
                key={proxy.name}
                role="button"
                tabIndex={0}
                className="cursor-pointer border-t border-zinc-800 hover:bg-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-zinc-100"
                onClick={() => setSelected(proxy.name)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setSelected(proxy.name);
                  }
                }}
              >
                <th className="p-3 font-medium text-zinc-100">
                  {proxy.name}{" "}
                  <span className="font-mono text-zinc-500">
                    {proxy.protocol}
                  </span>
                </th>
                <td className="p-3">{units(proxy.protocol, proxy.metrics)}</td>
                <td className="p-3">
                  {formatBytes(proxy.metrics.client_to_upstream_bytes)} →{" "}
                  {formatBytes(proxy.metrics.upstream_to_client_bytes)}
                </td>
                <td className="p-3">
                  {proxy.metrics.rejection_denominator
                    ? `${(proxy.metrics.rejection_ratio * 100).toFixed(1)}%`
                    : "—"}{" "}
                  <span className="text-zinc-500">
                    ({proxy.metrics.filter_rejections} filter,{" "}
                    {proxy.metrics.capacity_rejections} capacity)
                  </span>
                </td>
                <td className="p-3">{proxy.metrics.upstream_failures}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {selected && (
        <History
          name={selected}
          rounds={history.data}
          loading={history.isLoading}
        />
      )}
    </section>
  );
}

function History({
  name,
  rounds,
  loading,
}: {
  name: string;
  rounds?: MetricRound[];
  loading: boolean;
}) {
  if (loading)
    return (
      <div className="border-t border-zinc-700 p-5 text-sm text-zinc-400">
        Loading round history…
      </div>
    );
  if (!rounds) return null;
  return (
    <div className="border-t border-zinc-700 p-5">
      <h2 className="mb-3 text-sm font-semibold text-zinc-100">
        {name} round history
      </h2>
      <div className="grid gap-4 lg:grid-cols-3">
        <Trend
          label="Client units"
          values={rounds.map(
            (round) =>
              round.metrics.requests ?? round.metrics.client_chunks ?? 0,
          )}
        />
        <Trend
          label="Forwarded bytes"
          values={rounds.map(
            (round) =>
              round.metrics.client_to_upstream_bytes +
              round.metrics.upstream_to_client_bytes,
          )}
        />
        <Trend
          label="Rejection ratio"
          values={rounds.map((round) => round.metrics.rejection_ratio * 100)}
          suffix="%"
        />
      </div>
    </div>
  );
}

function Trend({
  label,
  values,
  suffix = "",
}: {
  label: string;
  values: number[];
  suffix?: string;
}) {
  const maximum = Math.max(1, ...values);
  const points = values
    .map(
      (value, index) =>
        `${values.length === 1 ? 0 : (index / (values.length - 1)) * 100},${100 - (value / maximum) * 100}`,
    )
    .join(" ");
  return (
    <div className="border border-zinc-800 bg-zinc-900 p-3">
      <div className="mb-2 flex justify-between text-xs text-zinc-400">
        <span>{label}</span>
        <span>
          {values.at(-1)?.toFixed(1)}
          {suffix}
        </span>
      </div>
      <svg
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        className="h-24 w-full text-emerald-400"
        role="img"
        aria-label={`${label} by round`}
      >
        <polyline
          points={points}
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        />
      </svg>
    </div>
  );
}
