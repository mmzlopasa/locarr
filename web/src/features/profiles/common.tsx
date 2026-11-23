import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ErrorAlert } from "@/components/ui";

/** An API error, with the server's explanation for validation errors. */
export function ApiErrorDetail({ error }: { error: unknown }) {
  if (!error) return null;
  // ErrorAlert shows the server's reason (for example a validation message) under the sentence.
  return <ErrorAlert error={error} />;
}

/** A small label/value row used in the result cards. */
export function Fact({ label, children, testId }: { label: string; children: ReactNode; testId?: string }) {
  return (
    <div className="grid grid-cols-[8.5rem_1fr] items-baseline gap-2 py-1 text-[13px]">
      <dt className="text-[12px] text-faint">{label}</dt>
      <dd className="min-w-0 break-words text-fg" data-testid={testId}>
        {children}
      </dd>
    </div>
  );
}

export function Chip({ children, tone = "neutral" }: { children: ReactNode; tone?: "neutral" | "accent" | "danger" | "warning" | "info" }) {
  const tones = {
    neutral: "border-line-strong bg-surface-3 text-muted",
    accent: "border-accent/40 bg-success-bg text-accent",
    danger: "border-danger/40 bg-danger-bg text-danger",
    warning: "border-warning/40 bg-warning-bg text-warning",
    info: "border-info/40 bg-info-bg text-info",
  };
  return <span className={`inline-flex h-5 items-center rounded border px-1.5 font-mono text-[11px] ${tones[tone]}`}>{children}</span>;
}

/** A pill that says how a score adds up (+150, −10000). */
export function Score({ value }: { value: number }) {
  const { i18n } = useTranslation();
  const nf = new Intl.NumberFormat(i18n.language, { signDisplay: "exceptZero" });
  const tone = value > 0 ? "text-accent" : value < 0 ? "text-danger" : "text-faint";
  return <span className={`font-mono text-[12px] tabular-nums ${tone}`}>{nf.format(value)}</span>;
}
