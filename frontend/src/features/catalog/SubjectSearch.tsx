import { useEffect, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, Spinner, TextField } from '../../shared/ui';
import { catalogApi } from './api';
import type { Subject } from './types';

const MIN_CHARS = 2;
const MAX_CHARS = 100; // server limit for q
const DEBOUNCE_MS = 300;

type Results = { query: string; status: 'ok'; items: Subject[] } | { query: string; status: 'error'; message: string };

interface SubjectSearchProps {
  selected: Subject | null;
  onSelect: (subject: Subject | null) => void;
  error?: string;
}

/**
 * Find a subject by code or name. Uses GET /subjects?q=… (logged-in users).
 * Results are kept with the query they belong to, so a slow, older response
 * never replaces a newer one.
 */
export function SubjectSearch({ selected, onSelect, error }: SubjectSearchProps) {
  const [text, setText] = useState('');
  const [debounced, setDebounced] = useState('');
  const [results, setResults] = useState<Results | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(text.trim()), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [text]);

  useEffect(() => {
    if (debounced.length < MIN_CHARS) return;
    const controller = new AbortController();
    catalogApi.searchSubjects(debounced, controller.signal).then(
      (page) => setResults({ query: debounced, status: 'ok', items: page.data }),
      (failure: unknown) => {
        if (!controller.signal.aborted) setResults({ query: debounced, status: 'error', message: errorMessage(failure) });
      },
    );
    return () => controller.abort();
  }, [debounced]);

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
  const waiting = query.length >= MIN_CHARS && !current;

  return (
    <div>
      <TextField
        label="Subject"
        type="search"
        name="subject"
        autoComplete="off"
        hint="Type at least two letters of the subject name or code."
        value={text}
        maxLength={MAX_CHARS}
        onChange={(event) => setText(event.target.value)}
        error={error}
      />
      <div aria-live="polite">
        {waiting && <Spinner label="Searching subjects…" />}
        {current?.status === 'error' && <Alert>{current.message}</Alert>}
        {current?.status === 'ok' && current.items.length === 0 && <p className="muted">No subject matches “{query}”.</p>}
        {current?.status === 'ok' && current.items.length > 0 && (
          <ul className="choice-list" aria-label="Matching subjects">
            {current.items.map((subject) => (
              <li key={subject.id}>
                <button type="button" className="choice-list__item" onClick={() => onSelect(subject)}>
                  <strong>{subject.name}</strong> <span className="muted">{subject.code}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
