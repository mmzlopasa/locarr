/** Local-date helpers for the calendar (weeks start on Monday). */

export function ymd(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

export function addDays(d: Date, n: number): Date {
  const x = new Date(d);
  x.setDate(x.getDate() + n);
  return x;
}

export function startOfWeek(d: Date): Date {
  const x = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const offset = (x.getDay() + 6) % 7; // Monday = 0
  return addDays(x, -offset);
}

/** The 6×7 days shown for the month of d. */
export function monthGrid(d: Date): Date[] {
  const first = new Date(d.getFullYear(), d.getMonth(), 1);
  const start = startOfWeek(first);
  return Array.from({ length: 42 }, (_, i) => addDays(start, i));
}

export function weekDays(d: Date): Date[] {
  const start = startOfWeek(d);
  return Array.from({ length: 7 }, (_, i) => addDays(start, i));
}

export type CalendarView = "month" | "week" | "agenda";

/** The date range a view needs, inclusive. */
export function rangeFor(view: CalendarView, d: Date): { start: string; end: string } {
  if (view === "month") {
    const g = monthGrid(d);
    return { start: ymd(g[0]!), end: ymd(g[41]!) };
  }
  if (view === "week") {
    const w = weekDays(d);
    return { start: ymd(w[0]!), end: ymd(w[6]!) };
  }
  const today = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  return { start: ymd(today), end: ymd(addDays(today, 90)) };
}

/** Moves the anchor date one view step. */
export function step(view: CalendarView, d: Date, dir: 1 | -1): Date {
  if (view === "month") return new Date(d.getFullYear(), d.getMonth() + dir, 1);
  if (view === "week") return addDays(d, 7 * dir);
  return addDays(d, 90 * dir);
}
