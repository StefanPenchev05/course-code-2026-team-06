import { useCallback, useEffect, useRef, useState } from 'react';
import type { Page } from '../../shared/api/client';

/** Loads one page of a list starting at `offset`. Pass the signal on to api.get so stale requests are cancelled. */
export type PageFetcher<T> = (offset: number, signal?: AbortSignal) => Promise<Page<T>>;

export type PagedListState<T> =
  | { status: 'loading' }
  | { status: 'error'; error: unknown }
  | { status: 'ready'; items: T[]; hasMore: boolean };

/** What a background refresh changed in the list, for announcing it (see LiveAnnouncement). */
export interface ListChange {
  added: number;
  removed: number;
}

/**
 * Loads the first page of a paginated API list ({ data, pagination }) and lets
 * the user load more.
 *
 * - Wrap `fetchPage` in useCallback. When it changes, the first page loads again.
 * - To show a fresh loading state when a filter changes, give the component
 *   that calls this hook a `key` (for example key={status}) so it remounts.
 * - `reload()` fetches the first page again after a mutation. The current items
 *   stay visible until the new ones arrive.
 * - `refresh()` is for live updates (#93): it refetches in the background as many
 *   items as are shown (so "Show more" results stay), swaps them in place and sets
 *   `change`. A failure keeps what is shown; the next event or reconnection retries.
 *
 * State is only set in response callbacks, never synchronously inside the
 * effect, as the react-hooks lint rules require.
 */
export function usePagedList<T>(fetchPage: PageFetcher<T>, idOf: (item: T) => string = defaultId) {
  const [state, setState] = useState<PagedListState<T>>({ status: 'loading' });
  const [version, setVersion] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<unknown>(null);
  const [change, setChange] = useState<ListChange | null>(null);
  // What is on screen, for the background refresh.
  const shown = useRef(state);
  useEffect(() => {
    shown.current = state;
  }, [state]);

  useEffect(() => {
    const controller = new AbortController();
    fetchPage(0, controller.signal).then(
      (page) => setState({ status: 'ready', items: page.data, hasMore: page.pagination.has_more }),
      (error: unknown) => {
        if (!controller.signal.aborted) setState({ status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [fetchPage, version]);

  const reload = useCallback(() => {
    // After an error, show the loader again so "Try again" visibly does something.
    setState((previous) => (previous.status === 'error' ? { status: 'loading' } : previous));
    setMoreError(null);
    setVersion((current) => current + 1);
  }, []);

  const refresh = useCallback(() => {
    const before = shown.current;
    if (before?.status !== 'ready') return; // still loading or showing an error: nothing to keep
    const shownItems = before.items;
    void (async () => {
      const items: T[] = [];
      let hasMore = true;
      try {
        while (hasMore && (items.length === 0 || items.length < shownItems.length)) {
          const page = await fetchPage(items.length);
          items.push(...page.data);
          hasMore = page.pagination.has_more;
          if (page.data.length === 0) break;
        }
      } catch {
        return; // keep what is shown
      }
      // Only if nothing else (a filter change, "Show more", a reload) replaced the list meanwhile.
      if (shown.current !== before) return;
      const old = new Set(shownItems.map(idOf));
      const now = new Set(items.map(idOf));
      const added = items.filter((item) => !old.has(idOf(item))).length;
      const removed = shownItems.filter((item) => !now.has(idOf(item))).length;
      setState({ status: 'ready', items, hasMore });
      if (added || removed) setChange({ added, removed });
    })();
  }, [fetchPage, idOf]);

  function loadMore() {
    if (state.status !== 'ready' || loadingMore) return;
    setLoadingMore(true);
    setMoreError(null);
    fetchPage(state.items.length).then(
      (page) => {
        setState((previous) =>
          previous.status === 'ready'
            ? { status: 'ready', items: [...previous.items, ...page.data], hasMore: page.pagination.has_more }
            : previous,
        );
        setLoadingMore(false);
      },
      (error: unknown) => {
        setMoreError(error);
        setLoadingMore(false);
      },
    );
  }

  return { state, reload, refresh, change, loadMore, loadingMore, moreError };
}

/** Most lists have an `id`; others (e.g. feedback) pass their own key. */
function defaultId(item: unknown): string {
  return String((item as { id?: unknown }).id);
}
