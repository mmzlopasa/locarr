import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { errorDetail, errorMessage } from "./errors";

type Variant = "primary" | "secondary" | "danger" | "ghost";

const variants: Record<Variant, string> = {
  primary: "bg-accent text-accent-ink hover:bg-accent-hover border-transparent font-semibold",
  secondary: "bg-surface-3 text-fg hover:bg-line border-line-strong",
  danger: "bg-danger-bg text-danger hover:bg-danger hover:text-bg border-danger/40",
  ghost: "bg-transparent text-muted hover:text-fg hover:bg-surface-3 border-transparent",
};

export function Button({ variant = "secondary", className = "", busy, children, disabled, ...rest }:
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; busy?: boolean }) {
  return (
    <button
      type="button"
      {...rest}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-ui border px-3 text-[13px] transition-colors
        disabled:cursor-not-allowed disabled:opacity-50 ${variants[variant]} ${className}`}
    >
      {children}
    </button>
  );
}

const inputClass = `h-8 w-full rounded-ui border border-line-strong bg-surface-2 px-2.5 text-[13px] text-fg
  placeholder:text-faint focus:border-info focus:outline-none disabled:opacity-60`;

export function Field({ label, hint, children, htmlFor }: { label: string; hint?: ReactNode; children: ReactNode; htmlFor: string }) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={htmlFor} className="text-[12px] font-medium text-muted">
        {label}
      </label>
      {children}
      {hint ? <p className="text-[12px] text-faint">{hint}</p> : null}
    </div>
  );
}

export function TextInput({ label, hint, className = "", ...rest }: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: ReactNode }) {
  const id = useId();
  return (
    <Field label={label} hint={hint} htmlFor={rest.id ?? id}>
      <input id={rest.id ?? id} className={`${inputClass} ${className}`} {...rest} />
    </Field>
  );
}

export function SelectInput({ label, children, className = "", ...rest }: SelectHTMLAttributes<HTMLSelectElement> & { label: string }) {
  const id = useId();
  return (
    <Field label={label} htmlFor={rest.id ?? id}>
      <select id={rest.id ?? id} className={`${inputClass} ${className}`} {...rest}>
        {children}
      </select>
    </Field>
  );
}

export function Checkbox({ label, description, ...rest }: InputHTMLAttributes<HTMLInputElement> & { label: string; description?: string }) {
  const id = useId();
  return (
    <div className="flex items-start gap-2">
      <input id={id} type="checkbox" className="mt-0.5 size-4 accent-[var(--lc-accent)]"
        aria-describedby={description ? `${id}-d` : undefined} {...rest} />
      <div className="flex flex-col">
        <label htmlFor={id} className="text-[13px] text-fg">
          {label}
        </label>
        {description ? (
          <span id={`${id}-d`} className="text-[12px] text-faint">
            {description}
          </span>
        ) : null}
      </div>
    </div>
  );
}

export function Panel({ title, description, actions, children, className = "" }:
  { title?: string; description?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`rounded-ui border border-line bg-surface ${className}`}>
      {title ? (
        <header className="flex flex-wrap items-start justify-between gap-2 border-b border-line px-4 py-3">
          <div>
            <h2 className="text-[14px] font-semibold text-fg">{title}</h2>
            {description ? <p className="mt-0.5 text-[12px] text-muted">{description}</p> : null}
          </div>
          {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
        </header>
      ) : null}
      <div className="p-4">{children}</div>
    </section>
  );
}

export function PageHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-[18px] font-semibold tracking-tight text-fg">{title}</h1>
        {description ? <p className="mt-0.5 text-[13px] text-muted">{description}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}

type Tone = "neutral" | "success" | "danger" | "warning" | "info";
const tones: Record<Tone, string> = {
  neutral: "bg-surface-3 text-muted border-line-strong",
  success: "bg-success-bg text-success border-success/30",
  danger: "bg-danger-bg text-danger border-danger/30",
  warning: "bg-warning-bg text-warning border-warning/30",
  info: "bg-info-bg text-info border-info/30",
};

export function Badge({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <span className={`inline-flex h-5 items-center rounded border px-1.5 text-[11px] font-medium uppercase tracking-wide ${tones[tone]}`}>
      {children}
    </span>
  );
}

export function Alert({ tone = "danger", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <div role={tone === "danger" ? "alert" : "status"} className={`rounded-ui border px-3 py-2 text-[13px] ${tones[tone]}`}>
      {children}
    </div>
  );
}

/** Shows an API or network error in the user's language (nothing when err is empty). */
export function ErrorAlert({ error }: { error: unknown }) {
  const { t } = useTranslation();
  if (!error) return null;
  const detail = errorDetail(error);
  return (
    <Alert>
      {errorMessage(t, error)}
      {detail ? <span className="mt-1 block break-words font-mono text-[11px] opacity-80" data-testid="error-detail">{detail}</span> : null}
    </Alert>
  );
}

export function Spinner({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <div role="status" className="flex items-center gap-2 text-[13px] text-muted">
      <span className="size-3.5 animate-spin rounded-full border-2 border-line-strong border-t-accent" aria-hidden />
      {label ?? t("common.loading")}
    </div>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <p className="py-6 text-center text-[13px] text-faint">{children}</p>;
}

/** A delete-style button that asks once more inline before acting. */
export function ConfirmButton({ label, confirmLabel, onConfirm, busy, testId }:
  { label: string; confirmLabel: string; onConfirm: () => void; busy?: boolean; testId?: string }) {
  const { t } = useTranslation();
  const [asking, setAsking] = useState(false);
  if (!asking) {
    return (
      <Button variant="ghost" onClick={() => setAsking(true)} data-testid={testId}>
        {label}
      </Button>
    );
  }
  return (
    <span className="inline-flex items-center gap-1">
      <Button
        variant="danger"
        busy={busy}
        onClick={() => {
          onConfirm();
          setAsking(false);
        }}
        data-testid={testId ? `${testId}-confirm` : undefined}
      >
        {confirmLabel}
      </Button>
      <Button variant="ghost" onClick={() => setAsking(false)}>
        {t("common.cancel")}
      </Button>
    </span>
  );
}
