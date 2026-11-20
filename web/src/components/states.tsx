/** The states every surface owes the reader. Loading is the one `Loader`; these two are the outcomes. */

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
