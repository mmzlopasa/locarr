import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type MediaKind, type QualityDefinition } from "@/api/client";
import { Alert, Button, ConfirmButton, PageHeader, Panel, Spinner } from "@/components/ui";
import { ApiErrorDetail } from "./common";
import { useQualityDefinitions, useQualityNames } from "./hooks";
import { clampDefinition, previewRange } from "./logic";

const KINDS: MediaKind[] = ["movies", "series", "music"];
const defKey = (d: Pick<QualityDefinition, "kind" | "quality">) => `${d.kind}/${d.quality}`;

export function QualityDefinitionsPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const defs = useQualityDefinitions();
  const names = useQualityNames();
  const [kind, setKind] = useState<MediaKind>("movies");
  const [runtime, setRuntime] = useState(110);
  const [edits, setEdits] = useState<Record<string, QualityDefinition>>({});
  const [open, setOpen] = useState<string | null>(null);

  useEffect(() => setRuntime(kind === "movies" ? 110 : kind === "series" ? 45 : 45), [kind]);
  const rows = useMemo(() => (defs.data ?? []).filter((d) => d.kind === kind).map((d) => edits[defKey(d)] ?? d), [defs.data, kind, edits]);
  const dirty = Object.keys(edits).length;
  const scale = kind === "music" ? 60 : 400;
  const step = kind === "music" ? 0.1 : 0.5;

  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/qualitydefinitions", { body: { definitions: Object.values(edits) } })),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.qualityDefinitions, data);
      setEdits({});
    },
  });
  const reset = useMutation({
    mutationFn: async () => unwrap(await api.POST("/qualitydefinitions/reset", { body: { kind } })),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.qualityDefinitions, data);
      setEdits({});
    },
  });
  // Sliders keep min ≤ preferred ≤ max; typed numbers are taken as they are (the server checks them).
  const change = (d: QualityDefinition, field: "min" | "preferred" | "max", value: number, clamp = true) =>
    setEdits({ ...edits, [defKey(d)]: clamp ? clampDefinition(d, field, value) : { ...d, [field]: Math.max(0, value) } });

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("definitions.title")} description={t("definitions.subtitle")}
        actions={
          <>
            <ConfirmButton label={t("definitions.reset")} confirmLabel={t("definitions.resetConfirm", { kind: t(`mediaKinds.${kind}`) })}
              onConfirm={() => reset.mutate()} busy={reset.isPending} />
            <Button variant="primary" disabled={!dirty} busy={save.isPending} onClick={() => save.mutate()}>
              {dirty ? t("definitions.saveCount", { n: dirty }) : t("common.save")}
            </Button>
          </>
        } />
      <ApiErrorDetail error={save.error ?? reset.error ?? defs.error} />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="tablist" aria-label={t("profiles.kind")} className="inline-flex rounded-ui border border-line bg-surface p-0.5">
          {KINDS.map((k) => (
            <button key={k} type="button" role="tab" aria-selected={kind === k} onClick={() => setKind(k)}
              className={`rounded px-3 py-1 text-[13px] transition-colors ${kind === k ? "bg-surface-3 text-fg" : "text-muted hover:text-fg"}`}>
              {t(`mediaKinds.${k}`)}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2 text-[12px] text-muted">
          <span>{t("definitions.previewFor")}</span>
          {[22, 45, 110, 180].map((m) => (
            <button key={m} type="button" onClick={() => setRuntime(m)} aria-pressed={runtime === m}
              className={`rounded border px-2 py-0.5 font-mono ${runtime === m ? "border-accent text-fg" : "border-line text-muted hover:text-fg"}`}>
              {m} min
            </button>
          ))}
          <input type="number" min={1} max={1000} value={runtime} aria-label={t("definitions.runtime")} data-testid="runtime-input"
            onChange={(e) => setRuntime(Math.max(1, Number(e.target.value) || 1))}
            className="h-7 w-16 rounded border border-line-strong bg-surface-2 px-1.5 font-mono text-fg focus:border-info focus:outline-none" />
        </div>
      </div>
      <Alert tone="info">{t("definitions.hint")}</Alert>
      <Panel>
        {defs.isPending ? <Spinner /> : null}
        <ul className="flex flex-col divide-y divide-line">
          {rows.map((d) => {
            const k = defKey(d);
            const prev = previewRange(d, runtime, i18n.language);
            const pct = (v: number) => `${Math.min(100, (v / scale) * 100)}%`;
            const isOpen = open === k;
            return (
              <li key={k} data-testid={`def-${d.quality}`}>
                <button type="button" onClick={() => setOpen(isOpen ? null : k)} aria-expanded={isOpen}
                  className="grid w-full grid-cols-[9rem_1fr] items-center gap-3 py-2 text-left sm:grid-cols-[10rem_1fr_15rem]">
                  <span className={`text-[13px] ${edits[k] ? "text-accent" : "text-fg"}`}>{names.get(d.quality) ?? d.quality}</span>
                  <span className="relative h-2 rounded-full bg-surface-3" aria-hidden>
                    <span className="absolute inset-y-0 rounded-full bg-accent/35"
                      style={{ left: pct(d.min), right: d.max > 0 ? `calc(100% - ${pct(d.max)})` : "0%" }} />
                    {d.preferred > 0 ? <span className="absolute -top-0.5 h-3 w-0.5 rounded bg-accent" style={{ left: pct(d.preferred) }} /> : null}
                  </span>
                  <span className="col-span-2 font-mono text-[12px] text-muted sm:col-span-1 sm:text-right" data-testid="def-preview">
                    {prev.min} – {prev.max ?? t("definitions.noLimit")}
                    {prev.preferred ? <span className="text-faint"> · {t("definitions.pref", { size: prev.preferred })}</span> : null}
                  </span>
                </button>
                {isOpen ? (
                  <div className="grid grid-cols-1 gap-3 pb-3 sm:grid-cols-3">
                    {(["min", "preferred", "max"] as const).map((f) => (
                      <div key={f} className="flex flex-col gap-1">
                        <label className="flex items-baseline justify-between text-[12px] text-muted" htmlFor={`${k}-${f}`}>
                          <span>{t(`definitions.${f}`)}</span>
                          <span className="font-mono text-fg">
                            {d[f] > 0 || f === "min" ? t("definitions.mbPerMin", { value: d[f] }) : f === "max" ? t("definitions.noLimit") : t("definitions.none")}
                          </span>
                        </label>
                        <input id={`${k}-${f}`} type="range" min={0} max={scale} step={step} value={d[f]}
                          onChange={(e) => change(d, f, Number(e.target.value))} className="accent-[var(--lc-accent)]" />
                        <input type="number" min={0} max={2000} step={step} value={d[f]} aria-label={t(`definitions.${f}`)}
                          onChange={(e) => change(d, f, Number(e.target.value), false)}
                          className="h-7 rounded border border-line-strong bg-surface-2 px-1.5 font-mono text-[12px] text-fg focus:border-info focus:outline-none" />
                      </div>
                    ))}
                    <p className="text-[12px] text-faint sm:col-span-3">{t("definitions.zeroHint")}</p>
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      </Panel>
    </div>
  );
}
