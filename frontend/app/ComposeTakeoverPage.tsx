import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  APIError,
  applyCompose,
  discoverCompose,
  getComposeDeployments,
  restoreCompose,
  type ComposeCandidate,
  type ComposeDeployment,
  type ComposeProject,
} from "../lib/api";
import { queryClient } from "../lib/query-client";
import { toast } from "sonner";
import { defaultProtocolChoice, type ProtocolChoice } from "./protocol-choice";

interface Props {
  onUnauthorized: () => void;
}
type Choice = ProtocolChoice;
const reportedQueryErrors = new WeakSet<object>();

export function ComposeTakeoverPage({ onUnauthorized }: Props) {
  const [choices, setChoices] = useState<Record<string, Choice>>({});
  const [confirmation, setConfirmation] = useState(false);
  const discovery = useQuery({
    queryKey: ["compose-projects"],
    queryFn: discoverCompose,
    enabled: false,
  });
  const deployments = useQuery({
    queryKey: ["compose-deployments"],
    queryFn: getComposeDeployments,
  });
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ["compose-deployments"] });
    await queryClient.invalidateQueries({ queryKey: ["proxies"] });
  };
  const apply = useMutation({
    mutationFn: () => {
      if (!discovery.data)
        throw new Error("Discover projects before applying.");
      const selections = Object.entries(choices)
        .filter(([, choice]) => choice.selected)
        .map(([id, choice]) => ({
          id,
          protocol: choice.protocol,
          ...(choice.protocol === "http" ? { scheme: choice.scheme } : {}),
        }));
      return applyCompose(discovery.data.revision, selections);
    },
    onSuccess: async () => {
      setConfirmation(false);
      await refresh();
      toast.success("Takeover applied", {
        description: "Selected services are now managed by ctf-proxy.",
      });
    },
    onError: (error) => notifyMutationError(error, "Could not apply takeover"),
  });
  const restore = useMutation({
    mutationFn: ({ ids, all }: { ids: string[]; all: boolean }) =>
      restoreCompose(ids, all),
    onSuccess: async (_, { all }) => {
      await refresh();
      toast.success(all ? "Deployments restored" : "Deployment restored", {
        description: "The original Compose port mappings were restored.",
      });
    },
    onError: (error) =>
      notifyMutationError(error, "Could not restore deployment"),
  });

  useUnauthorizedHandler(
    [discovery.error, deployments.error, apply.error, restore.error],
    onUnauthorized,
  );
  useQueryErrorNotifications(discovery.error, deployments.error);

  const selectedCount = Object.values(choices).filter(
    (choice) => choice.selected,
  ).length;
  const updateChoice = (
    candidate: ComposeCandidate,
    value: Partial<Choice>,
  ) => {
    setChoices((current) => ({
      ...current,
      [candidate.id]: {
        ...defaultProtocolChoice(candidate),
        ...current[candidate.id],
        ...value,
      },
    }));
  };

  return (
    <main className="mx-auto w-full max-w-[1440px] px-8 pt-14 pb-8 max-lg:px-6 max-sm:px-4">
      <TakeoverHeader />
      <DiscoverySection
        isScanning={discovery.isFetching}
        onScan={() => void discovery.refetch()}
      />
      {discovery.data && (
        <ReviewSection
          projects={discovery.data.projects}
          choices={choices}
          selectedCount={selectedCount}
          isApplying={apply.isPending}
          confirmation={confirmation}
          onChoiceChange={updateChoice}
          onRequestApply={() => setConfirmation(true)}
          onConfirm={() => void apply.mutateAsync()}
          onCancel={() => setConfirmation(false)}
        />
      )}
      <ManagedDeploymentsSection
        deployments={deployments.data}
        isLoading={deployments.isLoading}
        isRestoring={restore.isPending}
        onRestore={(ids, all) => void restore.mutate({ ids, all })}
      />
    </main>
  );
}

function TakeoverHeader() {
  return (
    <header className="border-b border-zinc-700 pb-6">
      <p className="m-0 font-mono text-[11px] tracking-[.08em] text-zinc-400 uppercase">
        Attack & Defense CTF
      </p>
      <h1 className="mt-1.5 mb-0 text-3xl font-semibold text-zinc-100">
        Compose takeover
      </h1>
      <p className="mb-0 text-sm text-zinc-400">
        Move selected service ports to loopback and place ctf-proxy on their
        original public bindings.
      </p>
    </header>
  );
}

function DiscoverySection({
  isScanning,
  onScan,
}: {
  isScanning: boolean;
  onScan: () => void;
}) {
  return (
    <section
      className="border-b border-zinc-700 py-7"
      aria-labelledby="discover-heading"
    >
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 id="discover-heading" className="m-0 text-lg font-semibold">
            1. Discover
          </h2>
          <p className="mb-0 text-sm text-zinc-400">
            Scan immediate service directories under the configured Compose
            root. Startup never changes Docker state.
          </p>
        </div>
        <button
          className="min-h-9 rounded-md border border-zinc-600 px-3 text-sm font-semibold hover:border-zinc-100 disabled:opacity-60"
          onClick={onScan}
          disabled={isScanning}
        >
          {isScanning ? "Scanning…" : "Scan projects"}
        </button>
      </div>
    </section>
  );
}

function ReviewSection({
  projects,
  choices,
  selectedCount,
  isApplying,
  confirmation,
  onChoiceChange,
  onRequestApply,
  onConfirm,
  onCancel,
}: {
  projects: ComposeProject[];
  choices: Record<string, Choice>;
  selectedCount: number;
  isApplying: boolean;
  confirmation: boolean;
  onChoiceChange: (candidate: ComposeCandidate, value: Partial<Choice>) => void;
  onRequestApply: () => void;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <section
      className="border-b border-zinc-700 py-7"
      aria-labelledby="review-heading"
    >
      <h2 id="review-heading" className="m-0 text-lg font-semibold">
        2. Review & apply
      </h2>
      <p className="text-sm text-zinc-400">
        Only explicit externally published TCP ports can be selected. The
        replacement address is loopback-only.
      </p>
      <div className="grid gap-5">
        {projects.map((project) => (
          <ProjectCandidates
            key={project.compose_file}
            project={project}
            choices={choices}
            onChoiceChange={onChoiceChange}
          />
        ))}
      </div>
      <ApplyConfirmation
        selectedCount={selectedCount}
        isApplying={isApplying}
        confirmation={confirmation}
        onRequestApply={onRequestApply}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />
    </section>
  );
}

function ProjectCandidates({
  project,
  choices,
  onChoiceChange,
}: {
  project: ComposeProject;
  choices: Record<string, Choice>;
  onChoiceChange: (candidate: ComposeCandidate, value: Partial<Choice>) => void;
}) {
  return (
    <article className="rounded-md border border-zinc-700">
      <header className="border-b border-zinc-700 px-4 py-3">
        <h3 className="m-0 text-sm font-semibold">{project.name}</h3>
        <p className="m-0 font-mono text-[11px] text-zinc-400">
          {project.compose_file}
        </p>
      </header>
      {project.candidates.map((candidate) => (
        <CandidateRow
          key={candidate.id}
          candidate={candidate}
          choice={choices[candidate.id] ?? defaultProtocolChoice(candidate)}
          onChange={onChoiceChange}
        />
      ))}
    </article>
  );
}

function CandidateRow({
  candidate,
  choice,
  onChange,
}: {
  candidate: ComposeCandidate;
  choice: Choice;
  onChange: (candidate: ComposeCandidate, value: Partial<Choice>) => void;
}) {
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-center gap-3 border-b border-zinc-800 px-4 py-3 last:border-b-0 max-md:grid-cols-1">
      <input
        aria-label={`Select ${candidate.service} ${candidate.listen}`}
        type="checkbox"
        checked={choice.selected}
        disabled={!candidate.eligible}
        onChange={(event) =>
          onChange(candidate, { selected: event.target.checked })
        }
      />
      <div>
        <p className="m-0 text-sm text-zinc-100">
          {candidate.service} ·{" "}
          <span className="font-mono text-xs">
            {candidate.listen} → {candidate.upstream || candidate.target}
          </span>
        </p>
        {candidate.reason && (
          <p className="m-0 text-xs text-zinc-400">
            Skipped: {candidate.reason}
          </p>
        )}
      </div>
      {candidate.eligible && (
        <select
          value={choice.protocol}
          onChange={(event) =>
            onChange(candidate, {
              protocol: event.target.value as Choice["protocol"],
            })
          }
          disabled={!choice.selected}
          className="h-9 rounded border border-zinc-600 bg-zinc-950 px-2 text-sm"
        >
          <option value="tcp">TCP</option>
          <option value="http">HTTP</option>
        </select>
      )}
      {candidate.eligible && choice.protocol === "http" && (
        <select
          value={choice.scheme}
          onChange={(event) =>
            onChange(candidate, {
              scheme: event.target.value as Choice["scheme"],
            })
          }
          disabled={!choice.selected}
          className="h-9 rounded border border-zinc-600 bg-zinc-950 px-2 text-sm"
        >
          <option value="http">HTTP</option>
          <option value="https">HTTPS</option>
        </select>
      )}
    </div>
  );
}

function ApplyConfirmation({
  selectedCount,
  isApplying,
  confirmation,
  onRequestApply,
  onConfirm,
  onCancel,
}: {
  selectedCount: number;
  isApplying: boolean;
  confirmation: boolean;
  onRequestApply: () => void;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="mt-5 flex items-center gap-3">
      <button
        className="min-h-9 rounded-md border border-zinc-600 px-3 text-sm font-semibold hover:border-zinc-100 disabled:opacity-50"
        disabled={selectedCount === 0 || isApplying}
        onClick={onRequestApply}
      >
        Apply {selectedCount || ""} selected
      </button>
      {confirmation && (
        <div className="flex items-center gap-2 text-sm">
          <span>This recreates affected services and starts proxies.</span>
          <button
            className="rounded border border-zinc-400 px-2 py-1 font-semibold"
            onClick={onConfirm}
          >
            Confirm
          </button>
          <button className="underline" onClick={onCancel}>
            Cancel
          </button>
        </div>
      )}
    </div>
  );
}

function ManagedDeploymentsSection({
  deployments,
  isLoading,
  isRestoring,
  onRestore,
}: {
  deployments: ComposeDeployment[] | undefined;
  isLoading: boolean;
  isRestoring: boolean;
  onRestore: (ids: string[], all: boolean) => void;
}) {
  return (
    <section className="py-7" aria-labelledby="managed-heading">
      <h2 id="managed-heading" className="m-0 text-lg font-semibold">
        3. Managed deployments & restore
      </h2>
      <p className="text-sm text-zinc-400">
        Restoration is refused if a managed Compose file changed after takeover.
      </p>
      {isLoading && (
        <p className="text-sm text-zinc-400">Loading managed deployments…</p>
      )}
      {deployments?.length === 0 && (
        <p className="text-sm text-zinc-400">No managed Compose deployments.</p>
      )}
      {deployments && deployments.length > 0 && (
        <>
          <div className="grid gap-2">
            {deployments.map((deployment) => (
              <DeploymentRow
                key={deployment.id}
                deployment={deployment}
                isRestoring={isRestoring}
                onRestore={onRestore}
              />
            ))}
          </div>
          <button
            className="mt-4 rounded-md border border-zinc-600 px-3 py-2 text-sm font-semibold"
            disabled={isRestoring}
            onClick={() => {
              if (window.confirm("Restore every managed Compose deployment?"))
                onRestore([], true);
            }}
          >
            Restore all
          </button>
        </>
      )}
    </section>
  );
}

function DeploymentRow({
  deployment,
  isRestoring,
  onRestore,
}: {
  deployment: ComposeDeployment;
  isRestoring: boolean;
  onRestore: (ids: string[], all: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-md border border-zinc-700 p-3 max-sm:flex-col max-sm:items-start">
      <div>
        <p className="m-0 text-sm">
          {deployment.project} / {deployment.service} ·{" "}
          <span className="font-mono text-xs">
            {deployment.listen} → {deployment.upstream}
          </span>
        </p>
        <p className="m-0 text-xs text-zinc-400">
          {deployment.proxy} · {deployment.protocol.toUpperCase()} ·{" "}
          {deployment.state}
        </p>
      </div>
      <button
        className="rounded border border-zinc-600 px-2 py-1 text-sm disabled:opacity-50"
        disabled={deployment.state === "drifted" || isRestoring}
        onClick={() => {
          if (
            window.confirm(
              `Restore ${deployment.project}/${deployment.service}?`,
            )
          )
            onRestore([deployment.id], false);
        }}
      >
        Restore
      </button>
    </div>
  );
}

function useUnauthorizedHandler(errors: unknown[], onUnauthorized: () => void) {
  useEffect(() => {
    if (
      errors.some((error) => error instanceof APIError && error.status === 401)
    )
      onUnauthorized();
  }, [errors, onUnauthorized]);
}

function useQueryErrorNotifications(
  discoveryError: unknown,
  deploymentsError: unknown,
) {
  useEffect(() => {
    const notify = (error: unknown, title: string) => {
      if (!error || (error instanceof APIError && error.status === 401)) return;
      if (typeof error === "object" && error !== null) {
        if (reportedQueryErrors.has(error)) return;
        reportedQueryErrors.add(error);
      }
      toast.error(title, {
        description:
          error instanceof Error ? error.message : "Try again in a moment.",
      });
    };
    notify(discoveryError, "Could not scan projects");
    notify(deploymentsError, "Could not load managed deployments");
  }, [deploymentsError, discoveryError]);
}

function notifyMutationError(error: unknown, title: string) {
  if (!(error instanceof APIError && error.status === 401))
    toast.error(title, {
      description:
        error instanceof Error ? error.message : "Try again in a moment.",
    });
}
