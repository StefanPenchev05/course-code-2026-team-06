import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { catalogApi, SubjectSearch, type Faculty, type Subject } from '../features/catalog';
import { TutorResults } from '../features/tutors/TutorResults';
import type { TutorSearchFilters } from '../features/tutors/types';
import { SelectField } from '../shared/ui';

/**
 * Find a tutor (#29): choose a subject, optionally narrow by the tutor's
 * faculty and degree level. Filters live in the URL, so the back button and
 * shared links keep them.
 */
export function TutorSearchPage() {
  const { user } = useAuth();
  const [params, setParams] = useSearchParams();
  const subjectId = params.get('subject_id') ?? '';
  const facultyId = params.get('faculty_id') ?? '';
  const level = params.get('level') ?? '';

  // The chosen subject's name. After a reload only its ID is in the URL, so look it up.
  const [subject, setSubject] = useState<Subject | null>(null);
  const [faculties, setFaculties] = useState<Faculty[]>([]);

  useEffect(() => {
    if (!subjectId || subject?.id === subjectId) return;
    const controller = new AbortController();
    catalogApi.subject(subjectId, controller.signal).then(setSubject, () => {
      // Unknown or removed subject: drop it from the URL and let the student search again.
      if (!controller.signal.aborted) setParams({}, { replace: true });
    });
    return () => controller.abort();
  }, [subjectId, subject?.id, setParams]);

  useEffect(() => {
    const controller = new AbortController();
    // The faculty filter is optional; if the list can't load, the filter is just left out.
    catalogApi.faculties(controller.signal).then(setFaculties, () => undefined);
    return () => controller.abort();
  }, []);

  function setFilter(name: 'faculty_id' | 'level', value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(name, value);
    else next.delete(name);
    setParams(next, { replace: true });
  }

  function chooseSubject(chosen: Subject | null) {
    setSubject(chosen);
    const next = new URLSearchParams(params);
    if (chosen) next.set('subject_id', chosen.id);
    else next.delete('subject_id');
    setParams(next);
  }

  const shownSubject = subject?.id === subjectId ? subject : null;
  const filters: TutorSearchFilters | null = subjectId
    ? {
        subject_id: subjectId,
        ...(facultyId ? { faculty_id: facultyId } : {}),
        ...(level === 'BACHELOR' || level === 'MASTER' ? { level } : {}),
      }
    : null;

  return (
    <section className="page">
      <h1>Find a tutor</h1>
      <p>Tutors are students who were approved by a coordinator for a subject. You can book a tutor from any faculty if the subject is also part of your program.</p>

      <div className="filters">
        <SubjectSearch selected={shownSubject} onSelect={chooseSubject} programId={user?.program_id} />
        {subjectId && (
          <div className="filters__row">
            {faculties.length > 0 && (
              <SelectField label="Tutor's faculty" name="faculty_id" value={facultyId} onChange={(event) => setFilter('faculty_id', event.target.value)}>
                <option value="">Any faculty</option>
                {faculties.map((faculty) => (
                  <option key={faculty.id} value={faculty.id}>
                    {faculty.name}
                  </option>
                ))}
              </SelectField>
            )}
            <SelectField label="Tutor's degree" name="level" value={level} onChange={(event) => setFilter('level', event.target.value)}>
              <option value="">Bachelor or master</option>
              <option value="BACHELOR">Bachelor</option>
              <option value="MASTER">Master</option>
            </SelectField>
          </div>
        )}
      </div>

      {filters ? <TutorResults filters={filters} /> : <p className="muted">Choose a subject to see who can help you.</p>}
    </section>
  );
}
