import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  createProxy,
  deleteProxy,
  getFilters,
  getProxies,
  isUnauthorized,
  proxyDefinitionSchema,
  replaceProxy,
  scanProjects,
  type FilterView,
  type ProxyDefinition,
  type ProxyView,
} from "../lib/api";
import { queryClient } from "../lib/query-client";
import { toast } from "sonner";
import { ScanAndConfigureModal } from "./ScanAndConfigureModal";
import { supportsProtocol } from "./filter-compatibility";

// ProxiesPageProps contains the callback used when any management request expires.
interface ProxiesPageProps {
  onUnauthorized: () => void;
}

const emptyProxy: ProxyDefinition = {
  name: "",
  active: true,
  protocol: "tcp",
  listen: "",
  upstream: "",
  filters: [],
};

// ProxiesPage manages proxy selection, editing, creation, and deletion.
export function ProxiesPage({ onUnauthorized }: ProxiesPageProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [selectedName, setSelectedName] = useState<string | undefined>();
  const [isCreating, setIsCreating] = useState(false);
  const [scanModalOpen, setScanModalOpen] = useState(false);
  const focusedOnce = useRef<string | undefined>(undefined);
  const proxies = useQuery({ queryKey: ["proxies"], queryFn: getProxies });
  const filters = useQuery({ queryKey: ["filters"], queryFn: getFilters });
  // Scan once when Proxies opens; applying changes remains an explicit modal action.
  const discovery = useQuery({
    queryKey: ["scan-projects"],
    queryFn: scanProjects,
  });
  const requestedProxy = searchParams.get("proxy") ?? undefined;
  const selectedProxyName =
    requestedProxy &&
    proxies.data?.some((proxy) => proxy.name === requestedProxy)
      ? requestedProxy
      : selectedName;
  const selected = proxies.data?.find(
    (proxy) => proxy.name === selectedProxyName,
  );
  const showEditor =
    selected !== undefined || isCreating || proxies.data?.length === 0;

  // refresh invalidates shared proxy and metric views after a mutation.
  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["proxies"] }),
      queryClient.invalidateQueries({ queryKey: ["metrics"] }),
    ]);
  };
  const create = useMutation({
    mutationFn: createProxy,
    onSuccess: async (proxy) => {
      await refresh();
      toast.success("Proxy created", {
        description: `${proxy.name} is ready to manage.`,
      });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not create proxy", {
          description: "Check the values and try again.",
        });
    },
  });
  const replace = useMutation({
    mutationFn: ({
      name,
      definition,
    }: {
      name: string;
      definition: ProxyDefinition;
    }) => replaceProxy(name, definition),
    onSuccess: async (proxy) => {
      await refresh();
      toast.success("Proxy saved", {
        description: `${proxy.name} was updated.`,
      });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not save proxy", {
          description: "Check the values and try again.",
        });
    },
  });
  const remove = useMutation({
    mutationFn: deleteProxy,
    onSuccess: async (_, name) => {
      setSelectedName(undefined);
      setIsCreating(false);
      setSearchParams({});
      await refresh();
      toast.success("Proxy removed", { description: `${name} was removed.` });
    },
    onError: (error) => {
      if (!isUnauthorized(error))
        toast.error("Could not remove proxy", {
          description: "Try again in a moment.",
        });
    },
  });

  useEffect(() => {
    if (
      isUnauthorized(proxies.error) ||
      isUnauthorized(filters.error) ||
      isUnauthorized(discovery.error) ||
      isUnauthorized(create.error) ||
      isUnauthorized(replace.error) ||
      isUnauthorized(remove.error)
    ) {
      onUnauthorized();
    }
  }, [
    create.error,
    discovery.error,
    filters.error,
    onUnauthorized,
    proxies.error,
    remove.error,
    replace.error,
  ]);

  useEffect(() => {
    if (
      !requestedProxy ||
      selected?.name !== requestedProxy ||
      focusedOnce.current === requestedProxy
    )
      return;
    const heading = document.getElementById("proxy-editor-heading");
    if (!heading) return;
    focusedOnce.current = requestedProxy;
    heading.scrollIntoView({ behavior: "smooth", block: "start" });
    heading.focus({ preventScroll: true });
  }, [requestedProxy, selected?.name]);

  function selectProxy(name: string | undefined) {
    setSelectedName(name);
    setIsCreating(name === undefined);
    if (requestedProxy) setSearchParams({});
  }

  // save chooses create or replacement based on the current selection.
  async function save(definition: ProxyDefinition) {
    if (selected) {
      const replaced = await replace.mutateAsync({
        name: selected.name,
        definition,
      });
      setSelectedName(replaced.name);
      setSearchParams({ proxy: replaced.name });
    } else {
      const created = await create.mutateAsync(definition);
      setSelectedName(created.name);
      setIsCreating(false);
    }
  }

  // confirmDelete requires an explicit browser confirmation before removal.
  function confirmDelete() {
    if (selected && window.confirm(`Remove proxy “${selected.name}”?`)) {
      remove.mutate(selected.name);
    }
  }

  return (
    <main className="mx-auto w-full max-w-[1440px] px-8 pt-14 pb-8 max-lg:px-6 max-lg:pt-10 max-lg:pb-6 max-sm:px-4 max-sm:pt-8 max-sm:pb-4">
      <header className="flex min-h-26 items-end justify-between gap-4 border-b border-zinc-700 pb-6 max-sm:min-h-0 max-sm:items-start">
        <div>
          <p className="m-0 font-mono text-[11px] leading-none tracking-[0.08em] text-zinc-400 uppercase">
            Configuration
          </p>
          <h1 className="mt-1.5 mb-0 text-3xl font-semibold tracking-tight text-zinc-100">
            Proxies
          </h1>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Link
            className="min-h-9 rounded-md border border-zinc-600 bg-transparent px-3 py-2 text-sm font-semibold text-zinc-100 no-underline transition hover:border-zinc-100 hover:bg-zinc-900"
            to="/filters"
          >
            Filter library
          </Link>
          <button
            type="button"
            className="min-h-9 cursor-pointer rounded-md border border-zinc-600 bg-transparent px-3 text-sm font-semibold text-zinc-100 transition hover:border-zinc-100 hover:bg-zinc-900 disabled:cursor-wait disabled:opacity-60"
            onClick={() => setScanModalOpen(true)}
            disabled={discovery.isLoading}
          >
            Scan and configure
          </button>
          <button
            type="button"
            className="min-h-9 cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-primary"
            onClick={() => selectProxy(undefined)}
          >
            Add proxy
          </button>
        </div>
      </header>
      <div className="grid min-h-140 grid-cols-[minmax(250px,0.8fr)_minmax(0,1.2fr)] max-lg:min-h-0 max-lg:grid-cols-1">
        <section
          className="border-r border-zinc-700 px-6 max-lg:border-r-0 max-lg:border-b"
          aria-label="Configured proxies"
        >
          {proxies.isLoading && (
            <p className="m-0 text-sm text-zinc-400">Loading proxies…</p>
          )}
          {proxies.isError && !isUnauthorized(proxies.error) && (
            <p className="m-0 text-sm text-zinc-400">Unable to load proxies.</p>
          )}
          {proxies.data?.length === 0 && (
            <p className="m-0 text-sm text-zinc-400">No proxies configured.</p>
          )}
          <div className="-mx-6 grid">
            {proxies.data?.map((proxy) => (
              <ProxyDirectoryItem
                key={proxy.name}
                proxy={proxy}
                selected={selectedProxyName === proxy.name}
                onSelect={() => selectProxy(proxy.name)}
              />
            ))}
          </div>
        </section>
        {showEditor && (
          <section className="p-6" aria-labelledby="proxy-editor-heading">
            <h2
              id="proxy-editor-heading"
              tabIndex={-1}
              className="m-0 mb-5 text-base font-semibold text-zinc-100 outline-none"
            >
              {selected ? `Edit ${selected.name}` : "Add proxy"}
            </h2>
            <ProxyEditor
              key={selected?.name ?? "new"}
              initial={selected ? toDefinition(selected) : emptyProxy}
              isSaving={create.isPending || replace.isPending}
              onSave={save}
              onDelete={selected ? confirmDelete : undefined}
              isDeleting={remove.isPending}
              filters={filters.data ?? []}
              filtersUnavailable={filters.isError}
            />
          </section>
        )}
      </div>
      <ScanAndConfigureModal
        discovery={discovery}
        open={scanModalOpen}
        onClose={() => setScanModalOpen(false)}
        onUnauthorized={onUnauthorized}
      />
    </main>
  );
}

// ProxyDirectoryItem is the selectable summary row for one configured proxy.
function ProxyDirectoryItem({
  proxy,
  selected,
  onSelect,
}: {
  proxy: ProxyView;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <div
      className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-t border-zinc-700 px-6 py-3.5 transition ${selected ? "bg-zinc-900 shadow-[inset_2px_0_0_0_#f4f4f5]" : "bg-transparent hover:bg-zinc-900"}`}
    >
      <button
        type="button"
        className="grid cursor-pointer gap-1 text-left"
        onClick={onSelect}
      >
        <span className="text-sm text-zinc-100">{proxy.name}</span>
        <span className="font-mono text-[11px] leading-tight text-zinc-400">
          {proxy.protocol} · {proxy.listen}
        </span>
      </button>
      <span className="font-mono text-[11px] text-zinc-400">
        {proxy.filters.length}{" "}
        {proxy.filters.length === 1 ? "filter" : "filters"}
      </span>
    </div>
  );
}

// ProxyEditorProps defines the controlled proxy form and its mutation states.
interface ProxyEditorProps {
  initial: ProxyDefinition;
  isSaving: boolean;
  isDeleting: boolean;
  onSave: (definition: ProxyDefinition) => Promise<void>;
  onDelete?: () => void;
  filters: FilterView[];
  filtersUnavailable: boolean;
}

// ProxyEditor validates and submits a proxy definition together with filter choices.
function ProxyEditor({
  initial,
  isSaving,
  isDeleting,
  onSave,
  onDelete,
  filters,
  filtersUnavailable,
}: ProxyEditorProps) {
  const [draft, setDraft] = useState(initial);
  const [validationError, setValidationError] = useState<string | undefined>();
  const [selectedFilter, setSelectedFilter] = useState("");

  // update changes one draft field and clears the previous validation message.
  function update<K extends keyof ProxyDefinition>(
    key: K,
    value: ProxyDefinition[K],
  ) {
    setDraft((current) => ({ ...current, [key]: value }));
    setValidationError(undefined);
  }

  function addFilter() {
    const filter = filters.find((current) => current.name === selectedFilter);
    if (
      !filter ||
      draft.filters.includes(filter.name) ||
      !supportsProtocol(filter, draft.protocol)
    )
      return;
    update("filters", [...draft.filters, filter.name]);
    setSelectedFilter("");
  }

  function removeFilter(name: string) {
    update(
      "filters",
      draft.filters.filter((current) => current !== name),
    );
  }

  // submit performs client-side shape validation before calling the API mutation.
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const parsed = proxyDefinitionSchema.safeParse(draft);
    if (!parsed.success) {
      setValidationError("Name, listen address, and upstream are required.");
      toast.error("Proxy details are incomplete", {
        description: "Name, listen address, and upstream are required.",
      });
      return;
    }
    await onSave(parsed.data);
  }

  return (
    <form className="grid gap-6" onSubmit={(event) => void submit(event)}>
      <ProxyDetailsFields draft={draft} onUpdate={update} />
      <FilterAssignments
        draft={draft}
        filters={filters}
        filtersUnavailable={filtersUnavailable}
        selectedFilter={selectedFilter}
        onSelectedFilterChange={setSelectedFilter}
        onAttach={addFilter}
        onDetach={removeFilter}
      />
      {validationError && (
        <p className="m-0 text-sm text-zinc-200" role="alert">
          {validationError}
        </p>
      )}
      <ProxyEditorActions
        isSaving={isSaving}
        isDeleting={isDeleting}
        onDelete={onDelete}
      />
    </form>
  );
}

function ProxyDetailsFields({
  draft,
  onUpdate,
}: {
  draft: ProxyDefinition;
  onUpdate: <K extends keyof ProxyDefinition>(
    key: K,
    value: ProxyDefinition[K],
  ) => void;
}) {
  return (
    <>
      <div className="grid grid-cols-2 gap-4 max-sm:grid-cols-1">
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">
          Name
          <input
            className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10 disabled:cursor-not-allowed disabled:opacity-60"
            value={draft.name}
            onChange={(event) => onUpdate("name", event.target.value)}
            required
          />
        </label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">
          Protocol
          <select
            className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10"
            value={draft.protocol}
            onChange={(event) =>
              onUpdate(
                "protocol",
                event.target.value as ProxyDefinition["protocol"],
              )
            }
          >
            <option value="tcp">TCP</option>
            <option value="http">HTTP</option>
          </select>
        </label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">
          Listen
          <input
            className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10"
            value={draft.listen}
            onChange={(event) => onUpdate("listen", event.target.value)}
            placeholder="127.0.0.1:31337"
            required
          />
        </label>
        <label className="grid gap-1.5 text-xs font-semibold text-zinc-400">
          Upstream
          <input
            className="h-10 w-full rounded-md border border-zinc-600 bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10"
            value={draft.upstream}
            onChange={(event) => onUpdate("upstream", event.target.value)}
            placeholder="127.0.0.1:31338"
            required
          />
        </label>
      </div>
      <label className="flex items-center gap-2 text-sm text-zinc-100">
        <input
          className="accent-zinc-200"
          type="checkbox"
          checked={draft.active}
          onChange={(event) => onUpdate("active", event.target.checked)}
        />{" "}
        Start active
      </label>
    </>
  );
}

function FilterAssignments({
  draft,
  filters,
  filtersUnavailable,
  selectedFilter,
  onSelectedFilterChange,
  onAttach,
  onDetach,
}: {
  draft: ProxyDefinition;
  filters: FilterView[];
  filtersUnavailable: boolean;
  selectedFilter: string;
  onSelectedFilterChange: (value: string) => void;
  onAttach: () => void;
  onDetach: (name: string) => void;
}) {
  return (
    <fieldset className="grid gap-3 border-t border-zinc-700 pt-5">
      <legend className="px-0 text-sm font-semibold text-zinc-100">
        Filters
      </legend>
      <p className="m-0 text-xs leading-relaxed text-zinc-400">
        Choose from every available filter. The dropdown shows where each filter
        is already used; a filter can be shared by multiple proxies.
      </p>
      {filtersUnavailable && (
        <p className="m-0 text-xs text-zinc-400" role="alert">
          Available filters could not be loaded. Existing assignments are
          unchanged.
        </p>
      )}
      {!filtersUnavailable && (
        <div className="flex gap-2 max-sm:flex-col">
          <select
            aria-label="Available filters"
            className="h-10 min-w-0 flex-1 rounded-md border border-zinc-600 bg-zinc-950 px-2.5 text-sm text-zinc-100 outline-none transition focus:border-zinc-100 focus:ring-3 focus:ring-white/10"
            value={selectedFilter}
            onChange={(event) => onSelectedFilterChange(event.target.value)}
          >
            <option value="">Select a filter to attach</option>
            {filters.map((filter) => (
              <FilterOption key={filter.name} filter={filter} draft={draft} />
            ))}
          </select>
          <button
            type="button"
            className="min-h-10 shrink-0 cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-primary disabled:cursor-not-allowed disabled:opacity-50"
            onClick={onAttach}
            disabled={!selectedFilter}
          >
            Attach filter
          </button>
        </div>
      )}
      <AttachedFilters names={draft.filters} onDetach={onDetach} />
    </fieldset>
  );
}

function FilterOption({
  filter,
  draft,
}: {
  filter: FilterView;
  draft: ProxyDefinition;
}) {
  const attached = draft.filters.includes(filter.name);
  const compatible = supportsProtocol(filter, draft.protocol);
  return (
    <option value={filter.name} disabled={attached || !compatible}>
      {filterOptionLabel(filter, attached, compatible, draft.protocol)}
    </option>
  );
}

function AttachedFilters({
  names,
  onDetach,
}: {
  names: string[];
  onDetach: (name: string) => void;
}) {
  if (names.length === 0)
    return <p className="m-0 text-sm text-zinc-400">No filters attached.</p>;
  return (
    <ul className="m-0 grid list-none gap-2 p-0">
      {names.map((name) => (
        <li
          key={name}
          className="flex items-center justify-between gap-3 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2"
        >
          <span className="min-w-0 break-words font-mono text-xs text-zinc-200">
            {name}
          </span>
          <button
            type="button"
            className="shrink-0 cursor-pointer bg-transparent p-0 text-xs font-semibold underline underline-offset-3 transition link-danger"
            onClick={() => onDetach(name)}
          >
            Detach
          </button>
        </li>
      ))}
    </ul>
  );
}

function ProxyEditorActions({
  isSaving,
  isDeleting,
  onDelete,
}: {
  isSaving: boolean;
  isDeleting: boolean;
  onDelete?: () => void;
}) {
  return (
    <div className="flex items-center gap-2 border-t border-zinc-700 pt-1">
      <button
        type="submit"
        className="min-h-9 cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-primary disabled:cursor-wait disabled:opacity-60"
        disabled={isSaving}
      >
        {isSaving ? "Saving…" : "Save proxy"}
      </button>
      {onDelete && (
        <button
          type="button"
          className="ml-auto min-h-9 cursor-pointer rounded-md border px-3 text-sm font-semibold transition button-danger disabled:cursor-wait disabled:opacity-60"
          onClick={onDelete}
          disabled={isDeleting}
        >
          {isDeleting ? "Removing…" : "Remove proxy"}
        </button>
      )}
    </div>
  );
}

// toDefinition removes runtime-only proxy state before populating the editor.
function toDefinition(proxy: ProxyView): ProxyDefinition {
  return proxyDefinitionSchema.parse(proxy);
}

function filterOptionLabel(
  filter: FilterView,
  attached: boolean,
  compatible: boolean,
  protocol: ProxyDefinition["protocol"],
): string {
  const assignment =
    filter.assigned_proxies.length === 0
      ? "unassigned"
      : `used by ${filter.assigned_proxies.join(", ")}`;
  if (attached) return `${filter.name} — attached to this proxy; ${assignment}`;
  if (!compatible)
    return `${filter.name} — incompatible with ${protocol.toUpperCase()}; ${assignment}`;
  return `${filter.name} — ${assignment}`;
}
