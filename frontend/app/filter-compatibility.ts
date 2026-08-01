import type { FilterView, ProxyView } from "../lib/api";

// supportsProtocol reports whether a filter can evaluate a proxy's protocol.
// Filters with no protocol requirement are compatible with every proxy.
export function supportsProtocol(
  filter: Pick<FilterView, "protocols">,
  protocol: ProxyView["protocol"],
): boolean {
  return filter.protocols.length === 0 || filter.protocols.includes(protocol);
}
