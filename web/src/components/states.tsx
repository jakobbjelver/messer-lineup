/** The three states every surface owes the reader: loading, empty, failed. One sentence each. */

export function Skeleton() {
  return (
    <div aria-busy="true" aria-live="polite">
      <span className="sr-only">Loading the board…</span>
      <div className="sk">
        <div className="sk__sign" />
        <div className="sk__count" />
        {[0, 1, 2, 3, 4, 5].map((index) => (
          <div key={index}>
            {index % 3 === 0 ? <div className="sk__day" /> : null}
            <div className="sk__row">
              <div className="sk__circle" />
              <div>
                <div className="sk__line sk__line--a" />
                <div className="sk__line sk__line--b" />
              </div>
              <div />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

type EmptyProps = {
  title: string;
  action?: { label: string; onClick: () => void };
  secondary?: { label: string; onClick: () => void };
  detail?: string;
};

export function Empty({ title, detail, action, secondary }: EmptyProps) {
  return (
    <div className="state">
      <h2>{title}</h2>
      {detail ? <p>{detail}</p> : null}
      {action || secondary ? (
        <div className="state__acts">
          {action ? (
            <button className="btn btn--primary" onClick={action.onClick}>
              {action.label}
            </button>
          ) : null}
          {secondary ? (
            <button className="btn" onClick={secondary.onClick}>
              {secondary.label}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function Failed({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="state">
      <h2>Couldn&rsquo;t load the board.</h2>
      <p>{message}</p>
      <div className="state__acts">
        <button className="btn btn--primary" onClick={onRetry}>
          Try again
        </button>
      </div>
    </div>
  );
}
