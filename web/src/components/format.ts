/** Formats a byte count with binary units (e.g. "1.5 TiB"). */
export function formatBytes(n: number | undefined, locale: string): string {
  if (n === undefined || !Number.isFinite(n)) return "–";
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: i === 0 ? 0 : 1 }).format(v)} ${units[i]}`;
}

export function formatDateTime(iso: string | undefined, locale: string): string {
  if (!iso) return "–";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "–";
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(d);
}

/** "3 min ago" / "in 2 h" style relative time. */
export function formatRelative(iso: string | undefined, locale: string, now = Date.now()): string {
  if (!iso) return "–";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "–";
  const diff = (t - now) / 1000;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (abs < 60) return rtf.format(Math.round(diff), "second");
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  return rtf.format(Math.round(diff / 86400), "day");
}

export function formatDuration(seconds: number, locale: string): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const nf = new Intl.NumberFormat(locale);
  const parts: string[] = [];
  if (d) parts.push(`${nf.format(d)} d`);
  if (h || d) parts.push(`${nf.format(h)} h`);
  parts.push(`${nf.format(m)} min`);
  return parts.join(" ");
}
