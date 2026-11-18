import { addDays, dayOf, dateOf, monthShort } from "../lib/indexData";
import type { Row } from "../lib/indexData";

type Props = {
  from: string;
  days: number;
  rows: Row[];
  /** The miner's build date, in the same `YYYY-MM-DD` space as the rows. */
  builtOn: string;
};

/**
 * The season ribbon: one column per day of the window, inked where the board has a show.
 *
 * The month spans are computed from the window rather than hard-coded — the window is the manifest's, and a
 * run that publishes 90 days must not draw a 120-column ribbon with empty months at the end.
 */
export default function Ribbon({ from, days, rows, builtOn }: Props) {
  const inked = new Set(rows.map((row) => dayOf(row.d)));

  const columns = Array.from({ length: days }, (_, i) => addDays(from, i));
  const monthSpans: { label: string; span: number }[] = [];
  for (const day of columns) {
    const label = monthShort(day);
    const last = monthSpans[monthSpans.length - 1];
    if (last && last.label === label) last.span += 1;
    else monthSpans.push({ label, span: 1 });
  }

  // The weekly ribbon is the phone view: 17 columns of a week each, inked if anything in that week has a show.
  const weeks = Math.ceil(days / 7);
  const dated = rows.map((row) => dateOf(dayOf(row.d)).getTime());
  const weekly = Array.from({ length: weeks }, (_, w) => {
    const start = dateOf(addDays(from, w * 7)).getTime();
    const end = start + 6 * 24 * 60 * 60 * 1000;
    const today = dated.length === 0 ? false : start <= dateOf(builtOn).getTime() && dateOf(builtOn).getTime() <= end;
    return { has: dated.some((t) => t >= start && t <= end), today };
  });

  return (
    <>
      <div className="ribbon ribbon--day" aria-hidden="true">
        {columns.map((day) => (
          <i key={day} className={day === builtOn ? "today" : inked.has(day) ? "on" : ""} />
        ))}
        {monthSpans.map((month) => (
          <span key={month.label} className="mo" style={{ gridColumn: `span ${month.span}` }}>
            {month.label}
          </span>
        ))}
      </div>
      <div className="ribbon ribbon--wk" aria-hidden="true">
        {weekly.map((week, i) => (
          <i key={i} className={week.today ? "today" : week.has ? "on" : ""} />
        ))}
      </div>
      <div className="ribcaption">
        <span>{`${dayNumberLabel(from)} ${monthShort(from)}`}</span>
        <span>
          <span className="num">{days}</span> days
        </span>
        <span>{`${dayNumberLabel(addDays(from, days))} ${monthShort(addDays(from, days))}`}</span>
      </div>
    </>
  );
}

function dayNumberLabel(day: string): string {
  return String(dateOf(day).getUTCDate()).padStart(2, "0");
}
