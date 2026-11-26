import { useEffect, useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router';
import { BookingForm } from '../features/bookings/BookingForm';
import type { Session } from '../features/bookings/types';
import { tutorsApi } from '../features/tutors/api';
import { FreeTimes } from '../features/tutors/FreeTimes';
import { ratingText } from '../features/tutors/format';
import type { FreeSlot, TutorProfile } from '../features/tutors/types';
import { ApiError } from '../shared/api/client';
import { errorMessage } from '../shared/api/errors';
import { Alert, PageLoader } from '../shared/ui';

type Load = { id: string; status: 'ok'; profile: TutorProfile } | { id: string; status: 'error'; error: unknown };

/** A tutor's public profile (#29): what they teach, aggregate rating and free times. No grades. */
export function TutorProfilePage() {
  const { id = '' } = useParams();
  const [params] = useSearchParams();
  const searchedSubject = params.get('subject_id');
  const [load, setLoad] = useState<Load | null>(null);
  // Booking (#32): step 1 picks a free time, step 2 fills in the form, step 3 sends it.
  const [chosen, setChosen] = useState<FreeSlot | null>(null);
  // The chosen time disappeared from the tutor's free times (live update, #92).
  const [unavailable, setUnavailable] = useState(false);
  const chosenRef = useRef<FreeSlot | null>(null);
  useEffect(() => {
    chosenRef.current = chosen;
  }, [chosen]);
  const [booked, setBooked] = useState<Session | null>(null);
  const [freeTimesVersion, setFreeTimesVersion] = useState(0);
  const bookedRef = useRef<HTMLDivElement>(null);

  // The confirmation is above the form the student just sent; move focus (and the view) to it.
  useEffect(() => {
    if (booked) bookedRef.current?.focus();
  }, [booked]);

  useEffect(() => {
    const controller = new AbortController();
    tutorsApi.profile(id, controller.signal).then(
      (profile) => setLoad({ id, status: 'ok', profile }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ id, status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [id]);

  const backLink = (
    <p>
      <Link to={searchedSubject ? `/tutors?subject_id=${encodeURIComponent(searchedSubject)}` : '/tutors'}>← Back to search</Link>
    </p>
  );

  const current = load?.id === id ? load : null;
  if (!current) return <PageLoader label="Loading tutor…" />;

  if (current.status === 'error') {
    // 404: not a tutor, or not one this student can book (inactive, or no shared program subject).
    const notFound = current.error instanceof ApiError && (current.error.status === 404 || current.error.status === 422);
    return (
      <section className="page">
        <h1>{notFound ? 'Tutor not available' : "We couldn't load this tutor"}</h1>
        <Alert kind={notFound ? 'info' : 'error'}>
          {notFound ? "This tutor isn't available to you right now. They may have stopped tutoring." : errorMessage(current.error)}
        </Alert>
        {backLink}
      </section>
    );
  }

  const { profile } = current;
  // Show the searched subject first.
  const offerings = [...profile.offerings].sort(
    (a, b) => Number(b.subject_id === searchedSubject) - Number(a.subject_id === searchedSubject),
  );

  return (
    <section className="page">
      {backLink}
      <h1>{profile.name}</h1>
      <p className="card__meta">{ratingText(profile)}</p>
      {profile.description && <p>{profile.description}</p>}

      <section aria-labelledby="offerings-heading">
        <h2 id="offerings-heading">Teaches</h2>
        <ul className="card-list">
          {offerings.map((offering) => (
            <li key={offering.id} className="card">
              <h3 className="card__title">{offering.subject_name}</h3>
              {/* For a first subject the API copies the description into the profile; don't repeat it. */}
              {offering.description && offering.description !== profile.description && <p>{offering.description}</p>}
            </li>
          ))}
        </ul>
      </section>

      {booked && (
        <div ref={bookedRef} tabIndex={-1}>
        <Alert kind="success" title="Request sent">
          <p>
            {profile.name} will confirm or decline your {booked.subject_name} session. You can follow it in{' '}
            <Link to="/sessions">My sessions</Link>.
          </p>
        </Alert>
        </div>
      )}

      <FreeTimes
        tutorId={profile.id}
        reloadKey={freeTimesVersion}
        onSelect={(slot) => {
          setBooked(null);
          setUnavailable(false);
          setChosen(slot);
        }}
        onSlots={(slots) => {
          // Re-check an open booking form against the fresh free times. Never claim the time was
          // booked by someone: the server only says it is no longer free.
          const current = chosenRef.current;
          if (!current) return;
          const start = new Date(current.starts_at).getTime();
          const same = slots.filter((slot) => slot.id === current.id);
          const fragment =
            same.find((slot) => new Date(slot.starts_at).getTime() <= start && start < new Date(slot.ends_at).getTime()) ??
            same.find((slot) => new Date(slot.ends_at).getTime() > start);
          setUnavailable(!fragment);
          if (fragment) setChosen(fragment);
        }}
      />

      {chosen && (
        <BookingForm
          // One form while booking this tutor: switching times keeps what was typed.
          key="booking"
          tutorName={profile.name}
          offerings={profile.offerings}
          window={chosen}
          unavailable={unavailable}
          subjectId={searchedSubject}
          onCancel={() => setChosen(null)}
          onBooked={(session) => {
            setChosen(null);
            setBooked(session);
            // The booked time is no longer free.
            setFreeTimesVersion((n) => n + 1);
          }}
        />
      )}
    </section>
  );
}
