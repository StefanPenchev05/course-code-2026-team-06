import { useEffect, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, Spinner, TextField } from '../../shared/ui';
import { catalogApi } from './api';
import type { Subject } from './types';

const MAX_CHARS = 100; // server limit for q
const DEBOUNCE_MS = 300;

type Results =
  | { query: string; status: 'ok'; items: Subject[]; hasMore: boolean }
  | { query: string; status: 'error'; message: string };

interface SubjectSearchProps {
  selected: Subject | null;
  onSelect: (subject: Subject | null) => void;
  /**
   * The student's program (from /auth/me). When set, the picker lists that
   * program's subjects right away (GET /programs/{id}/subjects) and typing
   * filters them. Without it, it searches all subjects (GET /subjects).
   */
  programId?: string;
  error?: string;
}

/**
 * Choose a subject by code or name. Results are kept with the query they
 * belong to, so a slow, older response never replaces a newer one.
 */
export function SubjectSearch({ selected, onSelect, programId, error }: SubjectSearchProps) {
  const ownProgram = Boolean(programId);
  // Listing a program's subjects needs no search text; searching everything needs two letters.
  const minChars = ownProgram ? 0 : 2;
  const [text, setText] = useState('');
  const [debounced, setDebounced] = useState('');
  const [results, setResults] = useState<Results | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(text.trim()), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [text]);

  useEffect(() => {
    if (debounced.length < minChars) return;
    const controller = new AbortController();
    const request = programId
      ? catalogApi.programSubjects(programId, debounced, 0, controller.signal)
      : catalogApi.searchSubjects(debounced, controller.signal);
    request.then(
      (page) => setResults({ query: debounced, status: 'ok', items: page.data, hasMore: page.pagination.has_more }),
      (failure: unknown) => {
        if (!controller.signal.aborted) setResults({ query: debounced, status: 'error', message: errorMessage(failure) });
      },
    );
    return () => controller.abort();
  }, [debounced, minChars, programId]);

  if (selected) {
    return (
      <div className="field">
        <span className="field__label">Subject</span>
        <p className="subject-chosen">
          <strong>{selected.name}</strong> <span className="muted">({selected.code})</span>{' '}
          <Button variant="link" onClick={() => onSelect(null)}>
            Change subject
          </Button>
        </p>
      </div>
    );
  }

  const query = text.trim();
  const current = results?.query === debounced && debounced === query ? results : null;
  const waiting = query.length >= minChars && !current;

  async function showMore() {
    if (!programId || current?.status !== 'ok') return;
    setLoadingMore(true);
    try {
      const page = await catalogApi.programSubjects(programId, current.query, current.items.length);
      setResults({ ...current, items: [...current.items, ...page.data], hasMore: page.pagination.has_more });
    } catch (failure) {
      setResults({ query: current.query, status: 'error', message: errorMessage(failure) });
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <div>
      <TextField
        label="Subject"
        type="search"
        name="subject"
        autoComplete="off"
        hint={
          ownProgram
            ? 'Subjects of your study program. Type to filter by name or code.'
            : 'Type at least two letters of the subject name or code.'
        }
        value={text}
        maxLength={MAX_CHARS}
        onChange={(event) => setText(event.target.value)}
        error={error}
      />
      <div aria-live="polite">
        {waiting && <Spinner label={ownProgram ? 'Loading subjects…' : 'Searching subjects…'} />}
        {current?.status === 'error' && <Alert>{current.message}</Alert>}
        {current?.status === 'ok' && current.items.length === 0 && (
          <p className="muted">
            {query ? `No subject matches “${query}”.` : 'Your program has no subjects listed yet.'}
          </p>
        )}
        {current?.status === 'ok' && current.items.length > 0 && (
          <>
            <ul className="choice-list" aria-label={ownProgram ? 'Subjects in your program' : 'Matching subjects'}>
              {current.items.map((subject) => (
                <li key={subject.id}>
                  <button type="button" className="choice-list__item" onClick={() => onSelect(subject)}>
                    <strong>{subject.name}</strong> <span className="muted">{subject.code}</span>
                  </button>
                </li>
              ))}
            </ul>
            {ownProgram && current.hasMore && (
              <Button variant="link" onClick={() => void showMore()} loading={loadingMore}>
                Show more subjects
              </Button>
            )}
          </>
        )}
      </div>
    </div>
  );
}
