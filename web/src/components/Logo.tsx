export function Logo({ compact = false }: { compact?: boolean }) {
  return (
    <span className="inline-flex items-center gap-2 text-fg">
      <svg viewBox="0 0 32 32" className="size-6" aria-hidden>
        <rect width="32" height="32" rx="7" fill="var(--lc-surface-3)" />
        <path d="M10 7v18h12" fill="none" stroke="var(--lc-accent)" strokeWidth="3.5" strokeLinecap="round" strokeLinejoin="round" />
        <circle cx="21" cy="11" r="3" fill="var(--lc-accent)" />
      </svg>
      {compact ? null : <span className="text-[15px] font-semibold tracking-tight">Locarr</span>}
    </span>
  );
}
