import { useEffect, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, SelectField, Spinner } from '../../shared/ui';
import { catalogApi } from './api';
import type { DegreeLevel, Faculty, Program } from './types';

const LEVEL_LABEL: Record<DegreeLevel, string> = { BACHELOR: 'Bachelor', MASTER: 'Master' };

type Load<T> = { key: string; status: 'ok'; items: T[] } | { key: string; status: 'error'; message: string };

interface ProgramPickerProps {
  facultyId: string;
  programId: string;
  onFacultyChange: (facultyId: string) => void;
  onProgramChange: (programId: string) => void;
  facultyError?: string;
  programError?: string;
  disabled?: boolean;
}

/**
 * Two linked dropdowns: choose a faculty, then one of its programs.
 * Uses the public catalog endpoints, so it works before logging in.
 * Results are stored with the key they were loaded for, so a stale
 * response never shows programs from a previously selected faculty.
 */
export function ProgramPicker({
  facultyId,
  programId,
  onFacultyChange,
  onProgramChange,
  facultyError,
  programError,
  disabled,
}: ProgramPickerProps) {
  const [attempt, setAttempt] = useState(0);
  const [faculties, setFaculties] = useState<Load<Faculty> | null>(null);
  const [programs, setPrograms] = useState<Load<Program> | null>(null);

  const facultiesKey = String(attempt);
  const programsKey = `${facultyId}#${attempt}`;

  useEffect(() => {
    const controller = new AbortController();
    catalogApi.publicFaculties(controller.signal).then(
      (items) => setFaculties({ key: facultiesKey, status: 'ok', items }),
      (error: unknown) => {
        if (!controller.signal.aborted) setFaculties({ key: facultiesKey, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [facultiesKey]);

  useEffect(() => {
    if (!facultyId) return;
    const controller = new AbortController();
    catalogApi.publicPrograms(facultyId, controller.signal).then(
      (items) => setPrograms({ key: programsKey, status: 'ok', items }),
      (error: unknown) => {
        if (!controller.signal.aborted) setPrograms({ key: programsKey, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [facultyId, programsKey]);

  const currentFaculties = faculties?.key === facultiesKey ? faculties : null;
  const currentPrograms = facultyId && programs?.key === programsKey ? programs : null;
  const loadError =
    (currentFaculties?.status === 'error' && currentFaculties.message) ||
    (currentPrograms?.status === 'error' && currentPrograms.message) ||
    null;

  if (loadError) {
    return (
      <Alert
        title="We couldn't load the list of programs"
        action={
          <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Button>
        }
      >
        {loadError}
      </Alert>
    );
  }

  if (!currentFaculties) return <Spinner label="Loading faculties…" />;

  const programItems = currentPrograms?.status === 'ok' ? currentPrograms.items : [];
  const programsLoading = Boolean(facultyId) && !currentPrograms;

  return (
    <>
      <SelectField
        label="Faculty"
        name="faculty"
        value={facultyId}
        onChange={(event) => {
          onFacultyChange(event.target.value);
          onProgramChange('');
        }}
        error={facultyError}
        disabled={disabled}
      >
        <option value="">Choose your faculty</option>
        {currentFaculties.status === 'ok' &&
          currentFaculties.items.map((faculty) => (
            <option key={faculty.id} value={faculty.id}>
              {faculty.name}
            </option>
          ))}
      </SelectField>

      <SelectField
        label="Study program"
        name="program"
        value={programId}
        onChange={(event) => onProgramChange(event.target.value)}
        error={programError}
        hint={facultyId ? undefined : 'Choose a faculty first.'}
        disabled={disabled || !facultyId || programsLoading}
      >
        <option value="">{programsLoading ? 'Loading programs…' : 'Choose your program'}</option>
        {programItems.map((program) => (
          <option key={program.id} value={program.id}>
            {program.name} ({LEVEL_LABEL[program.level] ?? program.level})
          </option>
        ))}
      </SelectField>
      {facultyId && currentPrograms?.status === 'ok' && programItems.length === 0 && (
        <Alert kind="info">This faculty has no programs open for registration yet.</Alert>
      )}
    </>
  );
}
