import { useId } from 'react';

const LABELS = ['Poor', 'Fair', 'Good', 'Very good', 'Excellent'];

/** A given rating: decorative stars plus the text "4 out of 5". */
export function RatingStars({ rating }: { rating: number }) {
  const filled = Math.max(0, Math.min(5, Math.round(rating)));
  return (
    <span className="rating-value">
      <span className="stars" aria-hidden="true">
        {'★'.repeat(filled)}
        {'☆'.repeat(5 - filled)}
      </span>{' '}
      {rating} out of 5
    </span>
  );
}

interface RatingFieldProps {
  value: number | null;
  onChange: (rating: number) => void;
  error?: string;
}

/** Five radio buttons (1 = poor … 5 = excellent). Arrow keys move between them. */
export function RatingField({ value, onChange, error }: RatingFieldProps) {
  const id = useId();
  const errorId = `${id}-error`;

  return (
    <fieldset className={['field', 'rating', error ? 'field--invalid' : ''].filter(Boolean).join(' ')}>
      <legend className="field__label">Rating</legend>
      <div className="rating__options">
        {LABELS.map((label, index) => {
          const score = index + 1;
          return (
            <label key={score} className="rating__option">
              <input
                type="radio"
                name={`${id}-rating`}
                value={score}
                checked={value === score}
                onChange={() => onChange(score)}
                aria-invalid={error ? true : undefined}
                aria-describedby={error ? errorId : undefined}
              />
              <span>
                {score} – {label}
              </span>
            </label>
          );
        })}
      </div>
      {error && (
        <p className="field__error" id={errorId}>
          {error}
        </p>
      )}
    </fieldset>
  );
}
