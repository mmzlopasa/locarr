import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Copy, Download, Plus, Trash2, Upload } from "lucide-react";
import { api, queryKeys, unwrap, type CustomFormat, type CustomFormatImplementation, type CustomFormatInput, type CustomFormatSpec } from "@/api/client";
import { Alert, Badge, Button, Checkbox, ConfirmButton, EmptyState, PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { ApiErrorDetail, Chip } from "./common";
import { useCustomFormats, useCustomFormatSchema, useLanguages } from "./hooks";
import { termError } from "./logic";

type Editing = CustomFormatInput & { id?: number };

export function CustomFormatsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const formats = useCustomFormats();
  const [editing, setEditing] = useState<Editing | null>(null);
  const [importing, setImporting] = useState(false);
  const [exported, setExported] = useState<{ name: string; json: string } | null>(null);
  const remove = useMutation({
    mutationFn: async (id: number) => unwrap(await api.DELETE("/customformats/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.customFormats }),
  });
  const doExport = useMutation({
    mutationFn: async (f: CustomFormat) => ({ name: f.name,
      json: JSON.stringify(unwrap(await api.GET("/customformats/{id}/export", { params: { path: { id: f.id } } })), null, 2) }),
    onSuccess: setExported,
  });

  if (editing) return <FormatEditor initial={editing} onDone={() => setEditing(null)} />;
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("formats.title")} description={t("formats.subtitle")}
        actions={
          <>
            <Button onClick={() => setImporting(!importing)} aria-expanded={importing}>
              <Upload className="size-4" />
              {t("formats.import")}
            </Button>
            <Button variant="primary" onClick={() => setEditing({ name: "", includeWhenRenaming: false,
              specifications: [{ name: "", implementation: "ReleaseTitleSpecification", negate: false, required: false, fields: { value: "" } }] })}>
              <Plus className="size-4" />
              {t("formats.add")}
            </Button>
          </>
        } />
      {importing ? <ImportPanel onDone={() => setImporting(false)} /> : null}
      {exported ? <ExportPanel name={exported.name} json={exported.json} onClose={() => setExported(null)} /> : null}
      <ApiErrorDetail error={formats.error ?? remove.error ?? doExport.error} />
      <Panel>
        {formats.isPending ? <Spinner /> : null}
        {formats.data?.length === 0 ? <EmptyState>{t("formats.none")}</EmptyState> : null}
        <ul className="flex flex-col divide-y divide-line" data-testid="format-list">
          {formats.data?.map((f) => (
            <li key={f.id} className="flex flex-wrap items-center gap-3 py-2" data-testid={`format-${f.name}`}>
              <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setEditing(f)}>
                <span className="block truncate text-[13px] font-medium text-fg">{f.name}</span>
                <span className="flex flex-wrap gap-1 pt-0.5">
                  {f.specifications.slice(0, 5).map((s, i) => (
                    <Chip key={i} tone={s.required ? "info" : "neutral"}>
                      {s.negate ? "¬ " : ""}{s.name || t(`formats.impl.${s.implementation}`, { defaultValue: s.implementation })}
                    </Chip>
                  ))}
                  {f.specifications.length > 5 ? <Chip>+{f.specifications.length - 5}</Chip> : null}
                </span>
              </button>
              {f.unsupported.length ? <Badge tone="warning">{t("formats.unsupportedCount", { n: f.unsupported.length })}</Badge> : null}
              <Button variant="ghost" onClick={() => doExport.mutate(f)}>
                <Download className="size-4" />
                {t("formats.export")}
              </Button>
              <ConfirmButton label={t("common.delete")} confirmLabel={t("formats.deleteConfirm", { name: f.name })} onConfirm={() => remove.mutate(f.id)} />
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  );
}

function ImportPanel({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [json, setJson] = useState("");
  const [flavor, setFlavor] = useState<"movies" | "series">("movies");
  const [replace, setReplace] = useState(false);
  const run = useMutation({
    mutationFn: async (dryRun: boolean) => unwrap(await api.POST("/customformats/import", { body: { json, flavor, replace, dryRun } })),
    onSuccess: (res) => {
      if (res.stored) {
        void qc.invalidateQueries({ queryKey: queryKeys.customFormats });
      }
    },
  });
  return (
    <Panel title={t("formats.importTitle")} description={t("formats.importHint")}>
      <div className="flex flex-col gap-3">
        <textarea value={json} onChange={(e) => setJson(e.target.value)} rows={10} spellCheck={false} aria-label={t("formats.importJson")}
          placeholder='{"name": "…", "specifications": [ … ]}' data-testid="import-json"
          className="w-full rounded-ui border border-line-strong bg-surface-2 p-2.5 font-mono text-[12px] text-fg placeholder:text-faint focus:border-info focus:outline-none" />
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-56">
            <SelectInput label={t("formats.flavor")} value={flavor} onChange={(e) => setFlavor(e.target.value as "movies" | "series")}>
              <option value="movies">{t("formats.flavors.movies")}</option>
              <option value="series">{t("formats.flavors.series")}</option>
            </SelectInput>
          </div>
          <Checkbox label={t("formats.replace")} checked={replace} onChange={(e) => setReplace(e.target.checked)} />
          <span className="flex-1" />
          <Button disabled={!json.trim()} busy={run.isPending && run.variables} onClick={() => run.mutate(true)}>{t("formats.check")}</Button>
          <Button variant="primary" disabled={!json.trim()} busy={run.isPending && !run.variables} onClick={() => run.mutate(false)}>{t("formats.importButton")}</Button>
        </div>
        <ApiErrorDetail error={run.error} />
        {run.data ? (
          <Alert tone={run.data.warnings.length ? "warning" : "success"}>
            <p>{run.data.stored ? t("formats.imported", { n: run.data.formats.length }) : t("formats.checked", { n: run.data.formats.length })}</p>
            {run.data.warnings.map((w) => <p key={w} className="font-mono text-[12px]">{w}</p>)}
            {run.data.stored ? <Button variant="ghost" onClick={onDone}>{t("common.close")}</Button> : null}
          </Alert>
        ) : null}
      </div>
    </Panel>
  );
}

function ExportPanel({ name, json, onClose }: { name: string; json: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const download = () => {
    const url = URL.createObjectURL(new Blob([json], { type: "application/json" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name.replace(/[^\w.-]+/g, "_")}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <Panel title={t("formats.exportTitle", { name })}
      actions={
        <>
          <Button onClick={() => void navigator.clipboard?.writeText(json).then(() => setCopied(true))}>
            <Copy className="size-4" />
            {copied ? t("formats.copied") : t("formats.copy")}
          </Button>
          <Button onClick={download}><Download className="size-4" />{t("formats.download")}</Button>
          <Button variant="ghost" onClick={onClose}>{t("common.close")}</Button>
        </>
      }>
      <pre className="max-h-80 overflow-auto rounded-ui bg-surface-2 p-2.5 font-mono text-[12px] text-fg" data-testid="export-json">{json}</pre>
    </Panel>
  );
}

function FormatEditor({ initial, onDone }: { initial: Editing; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const schema = useCustomFormatSchema();
  const [f, setF] = useState<Editing>(initial);
  const impls = schema.data?.implementations ?? [];
  const save = useMutation({
    mutationFn: async () => {
      const body: CustomFormatInput = { name: f.name, includeWhenRenaming: f.includeWhenRenaming, specifications: f.specifications };
      return f.id
        ? unwrap(await api.PUT("/customformats/{id}", { params: { path: { id: f.id } }, body }))
        : unwrap(await api.POST("/customformats", { body }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.customFormats });
      onDone();
    },
  });
  const setSpec = (i: number, s: CustomFormatSpec) => setF({ ...f, specifications: f.specifications.map((x, j) => (j === i ? s : x)) });
  return (
    <form className="flex flex-col gap-4" data-testid="format-editor" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
      <PageHeader title={f.id ? t("formats.edit", { name: initial.name }) : t("formats.new")}
        actions={
          <>
            <Button onClick={onDone}>{t("common.cancel")}</Button>
            <Button type="submit" variant="primary" busy={save.isPending} disabled={!f.name.trim() || f.specifications.length === 0}>{t("common.save")}</Button>
          </>
        } />
      <ApiErrorDetail error={save.error} />
      <Panel>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex-1">
            <TextInput label={t("formats.name")} value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} maxLength={100} required />
          </div>
          <Checkbox label={t("formats.renaming")} checked={!!f.includeWhenRenaming} onChange={(e) => setF({ ...f, includeWhenRenaming: e.target.checked })} />
        </div>
      </Panel>
      <Panel title={t("formats.conditions")} description={t("formats.conditionsHint")}
        actions={
          <Button onClick={() => setF({ ...f, specifications: [...f.specifications,
            { name: "", implementation: "ReleaseTitleSpecification", negate: false, required: false, fields: { value: "" } }] })}>
            <Plus className="size-4" />
            {t("formats.addCondition")}
          </Button>
        }>
        <ul className="flex flex-col gap-2">
          {f.specifications.map((s, i) => (
            <ConditionRow key={i} spec={s} impls={impls} onChange={(x) => setSpec(i, x)}
              onRemove={() => setF({ ...f, specifications: f.specifications.filter((_, j) => j !== i) })} />
          ))}
        </ul>
      </Panel>
    </form>
  );
}

function ConditionRow({ spec, impls, onChange, onRemove }: { spec: CustomFormatSpec; impls: CustomFormatImplementation[];
  onChange: (s: CustomFormatSpec) => void; onRemove: () => void }) {
  const { t } = useTranslation();
  const langs = useLanguages();
  const impl = impls.find((x) => x.implementation === spec.implementation);
  const fields = (spec.fields ?? {}) as Record<string, unknown>;
  const value = fields.value;
  const setField = (k: string, v: unknown) => onChange({ ...spec, fields: { ...fields, [k]: v } });
  const regexError = impl?.valueType === "regex" && typeof value === "string" && value ? termError(`/${value}/`) : null;
  let editor: React.ReactNode;
  switch (impl?.valueType) {
    case "regex":
      editor = (
        <TextInput label={t("formats.regex")} className="font-mono" value={String(value ?? "")} onChange={(e) => setField("value", e.target.value)}
          hint={regexError ? <span className="text-danger">{regexError}</span> : t("formats.regexHint")} />
      );
      break;
    case "enum":
    case "number":
      editor = (
        <SelectInput label={t("formats.value")} value={String(value ?? "")}
          onChange={(e) => setField("value", impl.valueType === "number" ? Number(e.target.value) : e.target.value)}>
          <option value="">—</option>
          {(impl.values ?? []).map((v) => <option key={v} value={v}>{t(`formats.values.${v}`, { defaultValue: v })}</option>)}
        </SelectInput>
      );
      break;
    case "language":
      editor = (
        <SelectInput label={t("formats.language")} value={String(value ?? "")} onChange={(e) => setField("value", e.target.value)}>
          <option value="">—</option>
          <option value="original">{t("profiles.original")}</option>
          <option value="any">{t("profiles.anyLanguage")}</option>
          {(langs.data ?? []).map((l) => <option key={l.code} value={l.code}>{t(`languageNames.${l.code}`, { defaultValue: l.name })}</option>)}
        </SelectInput>
      );
      break;
    case "range":
      editor = (
        <div className="grid grid-cols-2 gap-2">
          <TextInput label={spec.implementation === "SizeSpecification" ? t("formats.minGB") : t("formats.minYear")} type="number" min={0}
            value={Number(fields.min ?? 0)} onChange={(e) => setField("min", Number(e.target.value))} />
          <TextInput label={spec.implementation === "SizeSpecification" ? t("formats.maxGB") : t("formats.maxYear")} type="number" min={0}
            value={Number(fields.max ?? 0)} onChange={(e) => setField("max", Number(e.target.value))} hint={t("formats.zeroNoLimit")} />
        </div>
      );
      break;
    default:
      editor = <p className="text-[12px] text-warning">{t("formats.unsupportedHint", { name: spec.implementation })}</p>;
  }
  return (
    <li className="grid grid-cols-1 gap-3 rounded-ui border border-line bg-surface-2 p-3 md:grid-cols-[12rem_10rem_1fr_auto]" data-testid="condition">
      <SelectInput label={t("formats.type")} value={spec.implementation}
        onChange={(e) => {
          const next = impls.find((x) => x.implementation === e.target.value);
          onChange({ ...spec, implementation: e.target.value, fields: next?.valueType === "range" ? { min: 0, max: 0 } : { value: "" } });
        }}>
        {impls.map((x) => <option key={x.implementation} value={x.implementation}>{t(`formats.impl.${x.implementation}`, { defaultValue: x.implementation })}</option>)}
        {!impl ? <option value={spec.implementation}>{spec.implementation}</option> : null}
      </SelectInput>
      <TextInput label={t("formats.conditionName")} value={spec.name ?? ""} onChange={(e) => onChange({ ...spec, name: e.target.value })} />
      <div>{editor}</div>
      <div className="flex flex-row items-center gap-3 md:flex-col md:items-start">
        <Checkbox label={t("formats.required")} checked={!!spec.required} onChange={(e) => onChange({ ...spec, required: e.target.checked })} />
        <Checkbox label={t("formats.negate")} checked={!!spec.negate} onChange={(e) => onChange({ ...spec, negate: e.target.checked })} />
        <Button variant="ghost" onClick={onRemove} aria-label={t("formats.removeCondition")}><Trash2 className="size-4" /></Button>
      </div>
    </li>
  );
}
