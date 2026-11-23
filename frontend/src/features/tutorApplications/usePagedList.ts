import { useCallback, useEffect, useState } from 'react';
import type { Page } from '../../shared/api/client';

/** Loads one page of a list starting at `offset`. Pass the signal on to api.get so stale requests are cancelled. */
export type PageFetcher<T> = (offset: number, signal?: AbortSignal) => Promise<Page<T>>;

export type PagedListState<T> =
  | { status: 'loading' }
  | { status: 'error'; error: unknown }
  | { status: 'ready'; items: T[]; hasMore: boolean };

/**
 * Loads the first page of a paginated API list ({ data, pagination }) and lets
 * the user load more.
 *
 * - Wrap `fetchPage` in useCallback. When it changes, the first page loads again.
 * - To show a fresh loading state when a filter changes, give the component
 *   that calls this hook a `key` (for example key={status}) so it remounts.
 * - `reload()` fetches the first page again after a mutation. The current items
 *   stay visible until the new ones arrive.
 *
 * State is only set in response callbacks, never synchronously inside the
 * effect, as the react-hooks lint rules require.
 */
export function usePagedList<T>(fetchPage: PageFetcher<T>) {
  const [state, setState] = useState<PagedListState<T>>({ status: 'loading' });
  const [version, setVersion] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<unknown>(null);

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

  return { state, reload, loadMore, loadingMore, moreError };
}
