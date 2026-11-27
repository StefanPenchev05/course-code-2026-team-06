import { useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { useLiveRefresh } from '../../shared/realtime';
import { usePagedList } from './usePagedList';
import { Alert, Button, LiveAnnouncement, Spinner } from '../../shared/ui';
import type { Subject } from '../catalog';
import { applicationsApi } from './api';
import { ApplicationForm } from './ApplicationForm';
import { MyApplications } from './MyApplications';

/**
 * Student side of #30: apply for a subject and follow the decisions.
 * The backend allows one pending application at a time, so the form is
 * replaced by a notice while one is waiting for review.
 */
export function StudentApplications() {
  // applicationsApi.mine is a module-level function, so its identity never changes between renders.
  const { state, reload, refresh, change, loadMore, loadingMore, moreError } = usePagedList(applicationsApi.mine);
  const [submitted, setSubmitted] = useState<{ subject: Subject; id: string } | null>(null);
  // The coordinator's decision appears without a reload (#93).
  useLiveRefresh(['tutor_application.changed'], refresh);

  function handleSubmitted(subject: Subject, id: string) {
    setSubmitted({ subject, id });
    reload();
  }

  if (state.status === 'loading') return <Spinner label="Loading your applications…" />;
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

  // Lists are newest first, and only one application can be pending, so it is on the first page.
  const pending = state.items.find((application) => application.status === 'PENDING');
  // Once the application just sent has been decided, offer the form again.
  const sentAndWaiting = submitted !== null && !state.items.some((a) => a.id === submitted.id && a.status !== 'PENDING');

  return (
    <>
      <section aria-labelledby="apply-heading">
        <h2 id="apply-heading">Apply for a subject</h2>
        {submitted && sentAndWaiting ? (
          <Alert kind="success" title="Application sent">
            The coordinator of your faculty will review your application for {submitted.subject.name}. You'll get an email
            with the decision.
          </Alert>
        ) : pending ? (
          <Alert kind="info" title="You have an application waiting for review">
            Your application for {pending.subject_name} is with your faculty coordinator. You can apply for another
            subject once it has been decided.
          </Alert>
        ) : (
          <ApplicationForm onSubmitted={handleSubmitted} />
        )}
      </section>

      <section aria-labelledby="applications-heading">
        <h2 id="applications-heading">Your applications</h2>
        <LiveAnnouncement change={change} noun={['application', 'applications']} />
        <MyApplications
          applications={state.items}
          hasMore={state.hasMore}
          loadingMore={loadingMore}
          moreError={moreError}
          onLoadMore={loadMore}
        />
      </section>
    </>
  );
}
