import { useEffect, useState } from 'react';
import { useAuth } from '../auth/AuthContext';
import { catalogApi } from '../catalog';

/**
 * Names of the programs in the coordinator's faculty, by id. Verifications carry only a
 * program_id. Empty while loading or if the list could not be loaded.
 */
export function useProgramNames(): Map<string, string> {
  const { user } = useAuth();
  const facultyId = user?.coordinator_faculty_id ?? '';
  const [names, setNames] = useState<Map<string, string>>(() => new Map());

  useEffect(() => {
    if (!facultyId) return;
    const controller = new AbortController();
    catalogApi.publicPrograms(facultyId, controller.signal).then(
      (programs) => setNames(new Map(programs.map((program) => [program.id, program.name]))),
      () => {
        // The ID is still shown; a missing name is not worth an error message.
      },
    );
    return () => controller.abort();
  }, [facultyId]);

  return names;
}
