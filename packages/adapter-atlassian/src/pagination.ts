/**
 * Reusable pagination for Jira's classic startAt/maxResults/total/isLast
 * REST pagination shape (issue comments, worklogs, and this adapter's own
 * paginated issue search). A caller returning a bounded endpoint's result
 * to the model must never silently hand back only the first page as if it
 * were everything - PagedResult.complete/truncated_reason makes that
 * explicit instead.
 */

export interface PagedResult<T> {
  items: T[];
  complete: boolean;
  pages: number;
  /** Set only when complete is false: why collection stopped short of every page. */
  truncated_reason?: string;
}

export interface StartAtPage<T> {
  values: T[];
  total?: number;
  isLast?: boolean;
}

/** Default cap on total items collected across pages, absent a caller-specific override. */
export const DEFAULT_HARD_LIMIT_ITEMS = 1000;

/**
 * Walks fetchPage(startAt) until Jira reports isLast (or, lacking that, the
 * running item count reaches a reported total, or a page returns no
 * values), accumulating every page's values. Stops early - with
 * complete=false and a mandatory truncated_reason - once hardLimitItems is
 * reached, rather than ever presenting a partial result as if it were
 * everything.
 */
export async function collectStartAtPages<T>(
  fetchPage: (startAt: number) => Promise<StartAtPage<T>>,
  hardLimitItems: number = DEFAULT_HARD_LIMIT_ITEMS,
): Promise<PagedResult<T>> {
  const items: T[] = [];
  let startAt = 0;
  let pages = 0;

  for (;;) {
    const page = await fetchPage(startAt);
    pages += 1;
    items.push(...page.values);

    const noProgress = page.values.length === 0;
    const reachedTotal = page.total !== undefined && items.length >= page.total;
    if (page.isLast === true || noProgress || reachedTotal) {
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
    startAt += page.values.length;
  }
}
