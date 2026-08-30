/**
 * Reusable pagination for GitHub's two pagination styles: REST's Link
 * header (rel="next") and GraphQL's cursor/hasNextPage connections. A
 * caller returning a bounded endpoint's result to the model must never
 * silently hand back only the first page as if it were everything -
 * PagedResult.complete/truncated_reason makes that explicit instead.
 */

export interface PagedResult<T> {
  items: T[];
  complete: boolean;
  pages: number;
  /** Set only when complete is false: why collection stopped short of every page. */
  truncated_reason?: string;
}

/** Default cap on total items collected across pages, absent a caller-specific override. */
export const DEFAULT_HARD_LIMIT_ITEMS = 1000;

/** Extracts the rel="next" URL from a REST response's Link header, or undefined if there is none (the last page). */
export function parseNextLink(linkHeader: string | null | undefined): string | undefined {
  if (!linkHeader) return undefined;
  for (const part of linkHeader.split(',')) {
    const match = part.match(/<([^>]*)>\s*;\s*rel="next"/);
    if (match?.[1]) return match[1];
  }
  return undefined;
}

/**
 * Walks a REST endpoint's Link-header pagination starting at firstURL,
 * calling fetchPage for each URL in turn and accumulating every page's
 * items. Stops early - with complete=false and a mandatory
 * truncated_reason - once hardLimitItems is reached.
 */
export async function collectLinkPages<T>(
  firstURL: string,
  fetchPage: (url: string) => Promise<{ items: T[]; linkHeader?: string }>,
  hardLimitItems: number = DEFAULT_HARD_LIMIT_ITEMS,
): Promise<PagedResult<T>> {
  const items: T[] = [];
  let url: string | undefined = firstURL;
  let pages = 0;

  while (url) {
    const page = await fetchPage(url);
    pages += 1;
    items.push(...page.items);
    if (items.length >= hardLimitItems) {
      return {
        items,
        complete: false,
        pages,
        truncated_reason: `stopped after ${hardLimitItems} items across ${pages} pages (hard limit)`,
      };
    }
    url = parseNextLink(page.linkHeader);
  }
  return { items, complete: true, pages };
}

export interface CursorPage<T> {
  nodes: T[];
  endCursor?: string;
  hasNextPage: boolean;
}

/**
 * Walks a GraphQL connection's cursor pagination, calling fetchPage(after)
 * for each page and accumulating every page's nodes. Stops early - with
 * complete=false and a mandatory truncated_reason - once hardLimitItems is
 * reached.
 */
export async function collectCursorPages<T>(
  fetchPage: (after?: string) => Promise<CursorPage<T>>,
  hardLimitItems: number = DEFAULT_HARD_LIMIT_ITEMS,
): Promise<PagedResult<T>> {
  const items: T[] = [];
  let after: string | undefined;
  let pages = 0;

  for (;;) {
    const page = await fetchPage(after);
    pages += 1;
    items.push(...page.nodes);
    if (!page.hasNextPage || !page.endCursor) {
      return { items, complete: true, pages };
    }
    if (items.length >= hardLimitItems) {
      return {
        items,
        complete: false,
        pages,
        truncated_reason: `stopped after ${hardLimitItems} items across ${pages} pages (hard limit)`,
      };
    }
    after = page.endCursor;
  }
}
