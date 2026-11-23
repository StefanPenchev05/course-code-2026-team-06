import { useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from './usePagedList';
import { Alert, Button, Spinner } from '../../shared/ui';
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
  const { state, reload, loadMore, loadingMore, moreError } = usePagedList(applicationsApi.mine);
  const [submitted, setSubmitted] = useState<Subject | null>(null);

  function handleSubmitted(subject: Subject) {
    setSubmitted(subject);
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

  return (
    <>
      <section aria-labelledby="apply-heading">
        <h2 id="apply-heading">Apply for a subject</h2>
        {submitted ? (
          <Alert kind="success" title="Application sent">
            The coordinator of your faculty will review your application for {submitted.name}. You'll get an email with
            the decision.
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
