import { useCallback, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { Alert, Button, SelectField, Spinner } from '../../shared/ui';
import { useAuth } from '../auth/AuthContext';
import { catalogApi, SubjectSearch, type Program, type Subject } from '../catalog';
import { monitoringApi } from './api';
import { hoursText, ratingText } from './rules';

/**
 * Tutors of the coordinator's faculty, filtered by program and subject. The filters
 * live in the URL (?program_id=&subject_id=) so they survive opening a tutor and going back.
 */
export function TutorDirectory() {
  const { user } = useAuth();
  const [params, setParams] = useSearchParams();
  const programId = params.get('program_id') ?? '';
  const subjectId = params.get('subject_id') ?? '';
  const [programs, setPrograms] = useState<Program[] | null>(null);
  const [subject, setSubject] = useState<Subject | null>(null);
  const facultyId = user?.coordinator_faculty_id ?? '';

  useEffect(() => {
    if (!facultyId) return;
    const controller = new AbortController();
    catalogApi.publicPrograms(facultyId, controller.signal).then(
      (list) => setPrograms(list),
      () => {
        if (!controller.signal.aborted) setPrograms([]);
      },
    );
    return () => controller.abort();
  }, [facultyId]);

  // After a reload only the subject's id is in the URL; fetch its name for the filter.
  useEffect(() => {
    if (!subjectId || subject?.id === subjectId) return;
    const controller = new AbortController();
    catalogApi.subject(subjectId, controller.signal).then(
      (found) => setSubject(found),
      () => {
        if (!controller.signal.aborted) setSubject({ id: subjectId, code: '', name: 'The chosen subject' });
      },
    );
    return () => controller.abort();
  }, [subjectId, subject]);

  function setFilter(next: { program_id?: string; subject_id?: string }) {
    const merged = { program_id: programId, subject_id: subjectId, ...next };
    const query: Record<string, string> = {};
    if (merged.program_id) query.program_id = merged.program_id;
    if (merged.subject_id) query.subject_id = merged.subject_id;
    setParams(query, { replace: true });
  }

  const selectedSubject = subjectId && subject?.id === subjectId ? subject : null;

  return (
    <>
      <div className="filters">
        <SelectField
          label="Program"
          value={programId}
          disabled={programs === null}
          onChange={(event) => setFilter({ program_id: event.target.value })}
        >
          <option value="">All programs of your faculty</option>
          {programs?.map((program) => (
            <option key={program.id} value={program.id}>
              {program.name}
            </option>
          ))}
        </SelectField>
        {subjectId && !selectedSubject ? (
          <p className="muted">Loading the subject filter…</p>
        ) : (
          <SubjectSearch
            selected={selectedSubject}
            onSelect={(chosen) => {
              setSubject(chosen);
              setFilter({ subject_id: chosen?.id ?? '' });
            }}
          />
        )}
      </div>
      <TutorList key={`${programId}|${subjectId}`} programId={programId} subjectId={subjectId} />
    </>
  );
}

function TutorList({ programId, subjectId }: { programId: string; subjectId: string }) {
  const fetchTutors = useCallback(
    (offset: number, signal?: AbortSignal) => monitoringApi.tutors({ programId, subjectId }, offset, signal),
    [programId, subjectId],
  );
  const { state, reload, loadMore, loadingMore, moreError } = usePagedList(fetchTutors);

  if (state.status === 'loading') return <Spinner label="Loading tutors…" />;
  if (state.status === 'error') {
    return (
      <Alert
        action={
          <Button variant="secondary" onClick={reload}>
            Try again
          </Button>
        }
      >
        {errorMessage(state.error)}
      </Alert>
    );
  }
  if (state.items.length === 0) {
    return <p>{programId || subjectId ? 'No tutors match these filters.' : 'Your faculty has no approved tutors yet.'}</p>;
  }

  return (
    <>
      <ul className="card-list" aria-label="Tutors">
        {state.items.map((tutor) => (
          <li key={tutor.id} className="card">
            <div className="session__head">
              <h2 className="tutor-card__name">
                <Link to={`/coordinator/tutors/${tutor.id}`}>{tutor.name}</Link>
              </h2>
              <ActiveBadge active={tutor.active} />
            </div>
            <p className="card__meta">{tutor.program_name}</p>
            <p className="card__meta">
              {tutor.completed_sessions} completed {tutor.completed_sessions === 1 ? 'session' : 'sessions'} ·{' '}
              {hoursText(tutor.hours_taught)} h taught · {ratingText(tutor.average_rating, tutor.rating_count)}
            </p>
          </li>
        ))}
      </ul>
      {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
      {state.hasMore && (
        <Button variant="secondary" loading={loadingMore} onClick={loadMore}>
          Show more tutors
        </Button>
      )}
    </>
  );
}

export function ActiveBadge({ active }: { active: boolean }) {
  return <span className={`badge badge--${active ? 'active' : 'inactive'}`}>{active ? 'Active' : 'Not bookable'}</span>;
}
