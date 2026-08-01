import { useEffect, useMemo, useRef, useState } from "react";
import {
  useMutation,
  useQuery,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { ManagedFilterForm } from "./ManagedFilterForm";
import { supportsProtocol } from "./filter-compatibility";
import {
  createEmptyDraft,
  parseManagedFilterYAML,
  serializeManagedFilterYAML,
  type ManagedFilterDraft,
} from "./managed-filter-form";
import {
  createManagedFilter,
  deleteManagedFilter,
  getFilters,
  getManagedFilter,
  getProxies,
  isUnauthorized,
  replaceFilterAssignments,
  replaceManagedFilter,
  type FilterView,
  type ManagedFilterView,
  type ProxyView,
} from "../lib/api";
import { queryClient } from "../lib/query-client";

interface FiltersPageProps {
  onUnauthorized: () => void;
}

type Editor = { mode: "create" } | undefined;

// FiltersPage manages the filter library independently of any one proxy.
export function FiltersPage({ onUnauthorized }: FiltersPageProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [editor, setEditor] = useState<Editor>(undefined);
  const filters = useQuery({ queryKey: ["filters"], queryFn: getFilters });
  const proxies = useQuery({ queryKey: ["proxies"], queryFn: getProxies });
  const requestedFilter = searchParams.get("filter") ?? undefined;
  const selected = filters.data?.find(
    (filter) => filter.name === requestedFilter,
  );
  const managed = useQuery({
    queryKey: ["managed-filter", selected?.name],
    queryFn: () => getManagedFilter(selected?.name ?? ""),
    enabled: Boolean(selected?.editable),
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["filters"] }),
      queryClient.invalidateQueries({ queryKey: ["proxies"] }),
      queryClient.invalidateQueries({ queryKey: ["metrics"] }),
      queryClient.invalidateQueries({ queryKey: ["managed-filter"] }),
    ]);
  };
  const create = useMutation({
    mutationFn: (draft: ManagedFilterDraft) =>
      createManagedFilter(serializeManagedFilterYAML(draft)),
    onSuccess: async (filter) => {
      setEditor(undefined);
      setSearchParams({ filter: filter.name });
      await refresh();
      toast.success("Filter created", {
        description: `${filter.name} is ready to assign.`,
      });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not create filter", {
          description: "Check the values and try again.",
        });
    },
  });
  const replace = useMutation({
    mutationFn: ({
      name,
      draft,
    }: {
      name: string;
      draft: ManagedFilterDraft;
    }) => replaceManagedFilter(name, serializeManagedFilterYAML(draft)),
    onSuccess: async (_, { name }) => {
      await refresh();
      toast.success("Filter saved", { description: `${name} was updated.` });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not save filter", {
          description: "Check the values and try again.",
        });
    },
  });
  const assignments = useMutation({
    mutationFn: ({ name, proxies }: { name: string; proxies: string[] }) =>
      replaceFilterAssignments(name, proxies),
    onSuccess: async (filter) => {
      await refresh();
      toast.success("Assignments saved", {
        description:
          filter.assigned_proxies.length === 0
            ? `${filter.name} is no longer assigned.`
            : `${filter.name} is assigned to ${filter.assigned_proxies.length} ${filter.assigned_proxies.length === 1 ? "proxy" : "proxies"}.`,
      });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not save assignments", {
          description: "Check the selected proxies and try again.",
        });
    },
  });
  const remove = useMutation({
    mutationFn: deleteManagedFilter,
    onSuccess: async (_, name) => {
      setSearchParams({});
      await refresh();
      toast.success("Filter deleted", { description: `${name} was removed.` });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not delete filter", {
          description: "Unassign it from every proxy and try again.",
        });
    },
  });

  useEffect(() => {
    const errors = [
      filters.error,
      proxies.error,
      managed.error,
      create.error,
      replace.error,
      assignments.error,
      remove.error,
    ];
    if (errors.some(isUnauthorized)) onUnauthorized();
  }, [
    assignments.error,
    create.error,
    filters.error,
    managed.error,
    onUnauthorized,
    proxies.error,
    remove.error,
    replace.error,
  ]);

  function selectFilter(name: string) {
    setEditor(undefined);
    setSearchParams({ filter: name });
  }

  function startCreate() {
    setEditor({ mode: "create" });
    setSearchParams({});
  }

  function confirmDelete() {
    if (!selected || selected.assigned_proxies.length > 0) return;
    if (window.confirm(`Delete filter “${selected.name}”?`)) {
      remove.mutate(selected.name);
    }
  }

  const showDirectory =
    filters.isLoading || filters.isError || Boolean(filters.data?.length);

  return (
    <main className="mx-auto w-full max-w-[1440px] px-8 pt-14 pb-8 max-lg:px-6 max-lg:pt-10 max-lg:pb-6 max-sm:px-4 max-sm:pt-8 max-sm:pb-4">
      <header className="flex min-h-26 items-end justify-between gap-4 border-b border-zinc-700 pb-6 max-sm:min-h-0 max-sm:items-start">
        <div>
          <p className="m-0 font-mono text-[11px] leading-none tracking-[0.08em] text-zinc-400 uppercase">
            Configuration
          </p>
          <h1 className="mt-1.5 mb-0 text-3xl font-semibold tracking-tight text-zinc-100">
            Filters
          </h1>
        </div>
        <button
          type="button"
          className="min-h-9 cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-primary"
          onClick={startCreate}
        >
          Add filter
        </button>
      </header>
      <div
        className={`grid min-h-140 ${showDirectory ? "grid-cols-[minmax(250px,0.8fr)_minmax(0,1.2fr)] max-lg:min-h-0 max-lg:grid-cols-1" : "grid-cols-1"}`}
      >
        {showDirectory && (
          <FilterDirectory
            filters={filters}
            selectedName={selected?.name}
            onSelect={selectFilter}
          />
        )}
        <section
          className="p-6"
          aria-labelledby={
            editor?.mode === "create" ? undefined : "filter-editor-heading"
          }
          aria-label={editor?.mode === "create" ? "Add filter" : undefined}
        >
          {editor?.mode === "create" ? (
            <ManagedFilterForm
              key="create"
              initial={createEmptyDraft()}
              isExisting={false}
              isSaving={create.isPending}
              onSave={async (draft) => {
                await create.mutateAsync(draft);
              }}
              onCancel={() => setEditor(undefined)}
            />
          ) : selected ? (
            <FilterDetail
              filter={selected}
              proxies={proxies}
              managed={managed}
              isSaving={replace.isPending}
              isAssigning={assignments.isPending}
              isDeleting={remove.isPending}
              onSave={(draft) =>
                replace.mutateAsync({ name: selected.name, draft })
              }
              onAssignmentsSave={(proxyNames) =>
                assignments.mutateAsync({
                  name: selected.name,
                  proxies: proxyNames,
                })
              }
              onDelete={confirmDelete}
            />
          ) : (
            <EmptyDetail hasFilters={Boolean(filters.data?.length)} />
          )}
        </section>
      </div>
    </main>
  );
}

function FilterDirectory({
  filters,
  selectedName,
  onSelect,
}: {
  filters: UseQueryResult<FilterView[], Error>;
  selectedName?: string;
  onSelect: (name: string) => void;
}) {
  return (
    <section className="border-r border-zinc-700 px-6 max-lg:border-r-0 max-lg:border-b">
      {filters.isLoading && (
        <p className="m-0 text-sm text-zinc-400">Loading filters…</p>
      )}
      {filters.isError && !isUnauthorized(filters.error) && (
        <p className="m-0 text-sm text-zinc-400">Unable to load filters.</p>
      )}
      <div className="-mx-6 grid">
        {filters.data?.map((filter) => (
          <button
            key={filter.name}
            type="button"
            className={`grid cursor-pointer gap-1 border-t border-zinc-700 px-6 py-3.5 text-left transition ${selectedName === filter.name ? "bg-zinc-900 shadow-[inset_2px_0_0_0_#f4f4f5]" : "bg-transparent hover:bg-zinc-900"}`}
            onClick={() => onSelect(filter.name)}
          >
            <span className="text-sm text-zinc-100">{filter.name}</span>
            <span className="font-mono text-[11px] leading-tight text-zinc-400">
              {filter.source} · {filter.assigned_proxies.length}{" "}
              {filter.assigned_proxies.length === 1 ? "proxy" : "proxies"}
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}

function EmptyDetail({ hasFilters }: { hasFilters: boolean }) {
  return (
    <div className="grid min-h-80 place-content-center gap-2 text-center">
      <h2
        id="filter-editor-heading"
        className="m-0 text-base font-semibold text-zinc-100"
      >
        {hasFilters ? "Select a filter" : "Add a filter"}
      </h2>
      <p className="m-0 text-sm text-zinc-400">
        {hasFilters
          ? "Choose a filter to edit its definition and assignments."
          : "Create a reusable filter before assigning it to a proxy."}
      </p>
    </div>
  );
}

function FilterDetail({
  filter,
  proxies,
  managed,
  isSaving,
  isAssigning,
  isDeleting,
  onSave,
  onAssignmentsSave,
  onDelete,
}: {
  filter: FilterView;
  proxies: UseQueryResult<ProxyView[], Error>;
  managed: UseQueryResult<ManagedFilterView, Error>;
  isSaving: boolean;
  isAssigning: boolean;
  isDeleting: boolean;
  onSave: (draft: ManagedFilterDraft) => Promise<unknown>;
  onAssignmentsSave: (proxyNames: string[]) => Promise<unknown>;
  onDelete: () => void;
}) {
  return (
    <div className="grid gap-6">
      <header>
        <h2
          id="filter-editor-heading"
          className="m-0 text-base font-semibold text-zinc-100"
        >
          {filter.name}
        </h2>
        <p className="mt-1 mb-0 break-words font-mono text-[11px] leading-tight text-zinc-400">
          {filterMetadata(filter).join(" · ")}
        </p>
      </header>
      {filter.editable ? (
        <ManagedEditor
          filterName={filter.name}
          managed={managed}
          isSaving={isSaving}
          onSave={onSave}
        />
      ) : (
        <p className="m-0 border border-zinc-700 bg-zinc-900/40 px-4 py-3 text-sm text-zinc-400">
          This {filter.source} filter is read-only. Its proxy assignments can
          still be managed below.
        </p>
      )}
      <AssignmentPanel
        key={`${filter.name}:${filter.assigned_proxies.join(",")}`}
        filter={filter}
        proxies={proxies}
        isSaving={isAssigning}
        onSave={onAssignmentsSave}
      />
      {filter.editable && (
        <DeleteFilterAction
          filter={filter}
          isDeleting={isDeleting}
          onDelete={onDelete}
        />
      )}
    </div>
  );
}

function ManagedEditor({
  filterName,
  managed,
  isSaving,
  onSave,
}: {
  filterName: string;
  managed: UseQueryResult<ManagedFilterView, Error>;
  isSaving: boolean;
  onSave: (draft: ManagedFilterDraft) => Promise<unknown>;
}) {
  if (managed.isLoading)
    return (
      <p className="m-0 text-sm text-zinc-400">Loading filter configuration…</p>
    );
  if (managed.isError)
    return (
      <div className="grid gap-3 border border-zinc-700 px-4 py-4">
        <p className="m-0 text-sm text-zinc-400">
          Unable to load this filter configuration.
        </p>
        <button
          type="button"
          className="justify-self-start bg-transparent p-0 text-sm text-zinc-200 underline underline-offset-3"
          onClick={() => void managed.refetch()}
        >
          Retry
        </button>
      </div>
    );
  if (!managed.data || managed.data.name !== filterName) return null;
  let initial: ManagedFilterDraft;
  try {
    initial = parseManagedFilterYAML(managed.data.yaml);
  } catch {
    return (
      <p className="m-0 text-sm text-zinc-400" role="alert">
        This managed filter cannot be represented by the form.
      </p>
    );
  }
  return (
    <ManagedFilterForm
      key={filterName}
      initial={initial}
      isExisting
      assignedProxies={managed.data.assigned_proxies}
      isSaving={isSaving}
      onSave={async (draft) => {
        await onSave(draft);
      }}
    />
  );
}

function AssignmentPanel({
  filter,
  proxies,
  isSaving,
  onSave,
}: {
  filter: FilterView;
  proxies: UseQueryResult<ProxyView[], Error>;
  isSaving: boolean;
  onSave: (proxyNames: string[]) => Promise<unknown>;
}) {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string[]>(filter.assigned_proxies);
  const selector = useRef<HTMLDivElement>(null);
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const changed = !sameNames(selected, filter.assigned_proxies);

  useEffect(() => {
    if (!open) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    const closeOnOutsidePointer = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        !selector.current?.contains(event.target)
      ) {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", closeOnEscape);
    document.addEventListener("pointerdown", closeOnOutsidePointer);
    return () => {
      document.removeEventListener("keydown", closeOnEscape);
      document.removeEventListener("pointerdown", closeOnOutsidePointer);
    };
  }, [open]);

  function toggle(name: string) {
    setSelected((current) =>
      current.includes(name)
        ? current.filter((value) => value !== name)
        : [...current, name],
    );
  }

  const save = async () => {
    await onSave(selected);
    setOpen(false);
  };

  return (
    <section
      className="grid gap-3 border-t border-zinc-700 pt-5"
      aria-labelledby="filter-assignments"
    >
      <div>
        <h3
          id="filter-assignments"
          className="m-0 text-sm font-semibold text-zinc-100"
        >
          Proxy assignments
        </h3>
        <p className="mt-1 mb-0 text-xs leading-relaxed text-zinc-400">
          Save the complete assignment set at once. Active proxies whose chains
          change may restart.
        </p>
      </div>
      {proxies.isLoading && (
        <p className="m-0 text-sm text-zinc-400">Loading proxies…</p>
      )}
      {proxies.isError && !isUnauthorized(proxies.error) && (
        <p className="m-0 text-sm text-zinc-400">Unable to load proxies.</p>
      )}
      {proxies.data?.length === 0 && (
        <p className="m-0 text-sm text-zinc-400">
          Create a proxy before assigning this filter.
        </p>
      )}
      {proxies.data && proxies.data.length > 0 && (
        <div ref={selector} className="grid gap-3">
          <div className="relative justify-self-start">
            <button
              type="button"
              className="min-h-10 cursor-pointer rounded-md border border-zinc-600 bg-zinc-950 px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100"
              aria-expanded={open}
              aria-controls="filter-proxy-options"
              onClick={() => setOpen((current) => !current)}
            >
              Choose proxies · {selected.length} selected
            </button>
            {open && (
              <div
                id="filter-proxy-options"
                className="relative z-10 mt-2 grid w-90 max-w-[calc(100vw-4rem)] gap-px rounded-md border border-zinc-600 bg-zinc-950 p-1 shadow-xl"
                role="group"
                aria-label="Proxy assignments"
              >
                {proxies.data.map((proxy) => {
                  const checked = selectedSet.has(proxy.name);
                  const compatible = supportsProtocol(filter, proxy.protocol);
                  return (
                    <label
                      key={proxy.name}
                      className="flex cursor-pointer items-start gap-3 rounded px-3 py-2 text-sm hover:bg-zinc-900 has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-55"
                    >
                      <input
                        type="checkbox"
                        className="mt-0.5 accent-zinc-200"
                        checked={checked}
                        disabled={!checked && !compatible}
                        onChange={() => toggle(proxy.name)}
                      />
                      <span className="grid min-w-0 gap-1">
                        <span className="text-zinc-100">{proxy.name}</span>
                        <span className="font-mono text-[11px] leading-tight text-zinc-400">
                          {proxy.protocol} · {proxy.listen}
                          {!compatible
                            ? checked
                              ? " · incompatible (currently ineffective)"
                              : " · incompatible"
                            : ""}
                        </span>
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
          </div>
          <p className="m-0 text-xs text-zinc-400">
            {filter.assigned_proxies.length === 0
              ? "This filter is not assigned to any proxy."
              : `Currently assigned to: ${filter.assigned_proxies.join(", ")}.`}
          </p>
          <button
            type="button"
            className="min-h-9 justify-self-start cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-primary disabled:cursor-wait disabled:opacity-60"
            disabled={!changed || isSaving}
            onClick={() => void save()}
          >
            {isSaving ? "Saving assignments…" : "Save assignments"}
          </button>
        </div>
      )}
    </section>
  );
}

function DeleteFilterAction({
  filter,
  isDeleting,
  onDelete,
}: {
  filter: FilterView;
  isDeleting: boolean;
  onDelete: () => void;
}) {
  const assigned = filter.assigned_proxies.length > 0;
  return (
    <section className="grid gap-2 border-t border-zinc-700 pt-5">
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          className="min-h-9 cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-danger disabled:cursor-not-allowed disabled:opacity-60"
          disabled={assigned || isDeleting}
          onClick={onDelete}
        >
          {isDeleting ? "Deleting…" : "Delete filter"}
        </button>
        {assigned && (
          <p className="m-0 text-xs text-zinc-400">
            Unassign this filter from all proxies before deleting it.
          </p>
        )}
      </div>
    </section>
  );
}

function sameNames(left: string[], right: string[]): boolean {
  return (
    left.length === right.length && left.every((name) => right.includes(name))
  );
}

function filterMetadata(filter: FilterView): string[] {
  return [
    filter.source,
    filter.active ? "active" : "inactive",
    ...filter.protocols,
    ...filter.directions,
    filter.needs_http_body ? "HTTP body" : undefined,
  ].filter((value): value is string => Boolean(value));
}
