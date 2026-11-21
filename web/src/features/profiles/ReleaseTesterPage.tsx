import { useEffect, useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, ChevronRight, CircleAlert, Clock3, X } from "lucide-react";
import { api, unwrap, type ParseRequest, type ParseResult, type Rejection } from "@/api/client";
import { PageHeader, Panel, SelectInput, Spinner, TextInput } from "@/components/ui";
import { ApiErrorDetail, Chip, Fact, Score } from "./common";
import { useQualityProfiles } from "./hooks";
import { describeRejection } from "./rejections";

const EXAMPLES = [
  "Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.DV.HDR10.HEVC.TrueHD.7.1.Atmos-FGT",
  "The.Last.of.Us.S01E03.Long.Long.Time.1080p.AMZN.WEB-DL.DDP5.1.H.264-FLUX",
  "[SubsPlease] Sousou no Frieren - 12 (1080p) [A1B2C3D4].mkv",
  "Film.2021.HRV.1080p.WEB-DL.x264-GRP",
  "Pink Floyd - The Dark Side of the Moon (1973) [FLAC 24bit-96kHz]",
];

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

type Wanted = { title: string; year: string; season: string; episode: string; runtime: string };

export function ReleaseTesterPage() {
  const { t } = useTranslation();
  const profiles = useQualityProfiles();
  const [title, setTitle] = useState("");
  const [profileId, setProfileId] = useState<string>("");
  const [sizeGB, setSizeGB] = useState("");
  const [protocol, setProtocol] = useState<"torrent" | "usenet">("torrent");
  const [showWanted, setShowWanted] = useState(false);
  const [wanted, setWanted] = useState<Wanted>({ title: "", year: "", season: "", episode: "", runtime: "" });

  const body = useMemo<ParseRequest | null>(() => {
    if (!title.trim()) return null;
    const req: ParseRequest = { title: title.trim(), protocol };
    if (profileId) req.profileId = Number(profileId);
    const gb = Number(sizeGB.replace(",", "."));
    if (gb > 0) req.size = Math.round(gb * 1024 ** 3);
    if (profileId && showWanted) {
      const w: NonNullable<ParseRequest["wanted"]> = {};
      if (wanted.title.trim()) w.title = wanted.title.trim();
      if (Number(wanted.year) > 0) w.year = Number(wanted.year);
      if (Number(wanted.runtime) > 0) w.runtimeMinutes = Number(wanted.runtime);
      if (wanted.season !== "" && wanted.episode !== "") {
        w.episodes = [{ season: Number(wanted.season), episode: Number(wanted.episode) }];
      }
      req.wanted = w;
    }
    return req;
  }, [title, profileId, sizeGB, protocol, showWanted, wanted]);
  const debounced = useDebounced(body, 250);

  const result = useQuery({
    queryKey: ["parse", debounced],
    queryFn: async () => unwrap(await api.POST("/parse", { body: debounced as ParseRequest })),
    enabled: debounced !== null,
    placeholderData: keepPreviousData,
    retry: false,
  });

  const setW = (k: keyof Wanted) => (e: React.ChangeEvent<HTMLInputElement>) => setWanted({ ...wanted, [k]: e.target.value });

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("tester.title")} description={t("tester.subtitle")} />
      <Panel>
        <div className="flex flex-col gap-3">
          <label htmlFor="tester-input" className="text-[12px] font-medium text-muted">
            {t("tester.name")}
          </label>
          <input
            id="tester-input"
            data-testid="tester-input"
            autoFocus
            spellCheck={false}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={t("tester.placeholder")}
            className="h-11 w-full rounded-ui border border-line-strong bg-surface-2 px-3 font-mono text-[14px] text-fg placeholder:text-faint focus:border-info focus:outline-none"
          />
          <div className="flex flex-wrap gap-1.5" aria-label={t("tester.examples")}>
            {EXAMPLES.map((ex) => (
              <button key={ex} type="button" onClick={() => setTitle(ex)}
                className="max-w-full truncate rounded border border-line bg-surface-2 px-2 py-0.5 font-mono text-[11px] text-muted transition-colors hover:border-line-strong hover:text-fg">
                {ex}
              </button>
            ))}
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <SelectInput label={t("tester.profile")} value={profileId} onChange={(e) => setProfileId(e.target.value)} data-testid="tester-profile">
              <option value="">{t("tester.noProfile")}</option>
              {(profiles.data ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {t(`mediaKinds.${p.kind}`)} · {p.name}
                </option>
              ))}
            </SelectInput>
            <TextInput label={t("tester.size")} inputMode="decimal" placeholder="12.5" value={sizeGB} onChange={(e) => setSizeGB(e.target.value)} />
            <SelectInput label={t("tester.protocol")} value={protocol} onChange={(e) => setProtocol(e.target.value as "torrent" | "usenet")}>
              <option value="torrent">{t("profiles.protocols.torrent")}</option>
              <option value="usenet">{t("profiles.protocols.usenet")}</option>
            </SelectInput>
          </div>
          {profileId ? (
            <div>
              <button type="button" onClick={() => setShowWanted(!showWanted)} aria-expanded={showWanted}
                className="inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
                {showWanted ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                {t("tester.wanted")}
              </button>
              {showWanted ? (
                <div className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-5">
                  <div className="col-span-2">
                    <TextInput label={t("tester.wantedTitle")} value={wanted.title} onChange={setW("title")} />
                  </div>
                  <TextInput label={t("tester.wantedYear")} inputMode="numeric" value={wanted.year} onChange={setW("year")} />
                  <TextInput label={t("tester.wantedSeason")} inputMode="numeric" placeholder="S" value={wanted.season} onChange={setW("season")} />
                  <TextInput label={t("tester.wantedEpisode")} inputMode="numeric" placeholder="E" value={wanted.episode} onChange={setW("episode")} />
                  <TextInput label={t("tester.runtime")} inputMode="numeric" placeholder="110" value={wanted.runtime} onChange={setW("runtime")} />
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
      </Panel>
      {!body ? <p className="text-[13px] text-faint">{t("tester.empty")}</p> : null}
      {body && result.isPending ? <Spinner /> : null}
      <ApiErrorDetail error={result.error} />
      {body && result.data ? <Result data={result.data} withProfile={!!profileId} /> : null}
    </div>
  );
}

function Result({ data, withProfile }: { data: ParseResult; withProfile: boolean }) {
  const { t, i18n } = useTranslation();
  const r = data.release;
  const v = r.video;
  const a = r.audio;
  const lang = (codes?: string[]) => (codes ?? []).map((c) => t(`languageNames.${c}`, { defaultValue: c })).join(", ");
  const episodes = () => {
    const parts: string[] = [];
    if (r.seasons?.length) parts.push(t("tester.seasonList", { list: r.seasons.join(", ") }));
    if (r.episodes?.length) parts.push(t("tester.episodeList", { list: r.episodes.join(", ") }));
    if (r.absoluteEpisodes?.length) parts.push(t("tester.absoluteList", { list: r.absoluteEpisodes.join(", ") }));
    if (r.airDate) parts.push(r.airDate);
    return parts.join(" · ");
  };
  const packs = [r.fullSeason && t("tester.fullSeason"), r.multiSeason && t("tester.multiSeason"), r.completeSeries && t("tester.completeSeries"),
    r.special && t("tester.special"), r.anime && t("tester.anime")].filter(Boolean) as string[];
  const confidence = Math.round(r.confidence * 100);
  const matched = data.customFormats.filter((f) => f.matched);
  const others = data.customFormats.filter((f) => !f.matched);
  return (
    <div className="flex flex-col gap-4" data-testid="tester-result">
      {withProfile && data.decision ? <Verdict status={data.decision.status} rejections={data.decision.rejections} /> : null}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Panel title={t("tester.what")}>
          {r.obfuscated ? <p className="mb-2 text-[13px] text-warning">{t("tester.obfuscated")}</p> : null}
          <dl>
            <Fact label={t("tester.kind")}>
              <Chip tone="info">{t(`tester.kinds.${r.kind}`)}</Chip>
            </Fact>
            {r.music?.artist ? <Fact label={t("tester.artist")}>{r.music.artist}</Fact> : null}
            <Fact label={r.kind === "music" ? t("tester.album") : t("tester.titleField")} testId="tester-title">
              <span className="font-medium">{r.title || "–"}</span>
            </Fact>
            {r.alternateTitles?.length ? <Fact label={t("tester.alternate")}>{r.alternateTitles.join(" / ")}</Fact> : null}
            {r.year ? <Fact label={t("tester.year")}>{r.year}</Fact> : null}
            {episodes() ? <Fact label={t("tester.episodes")} testId="tester-episodes">{episodes()}</Fact> : null}
            {packs.length ? (
              <Fact label={t("tester.pack")}>
                <span className="flex flex-wrap gap-1">{packs.map((p) => <Chip key={p}>{p}</Chip>)}</span>
              </Fact>
            ) : null}
            {r.episodeTitle ? <Fact label={t("tester.episodeTitle")}>{r.episodeTitle}</Fact> : null}
            {r.editions?.length ? <Fact label={t("tester.editions")}>{r.editions.join(", ")}</Fact> : null}
            {r.threeD ? <Fact label="3D">{t("common.yes")}</Fact> : null}
            {r.music?.source || r.music?.discs ? (
              <Fact label={t("tester.musicSource")}>
                {[r.music?.source, r.music?.discs ? t("tester.discs", { n: r.music.discs }) : ""].filter(Boolean).join(" · ")}
              </Fact>
            ) : null}
            <Fact label={t("tester.group")} testId="tester-group">{r.group || "–"}</Fact>
            {r.hash ? <Fact label={t("tester.hash")}><span className="font-mono">{r.hash}</span></Fact> : null}
            <Fact label={t("tester.revision")}>
              {t("tester.version", { version: r.revision.version })}
              {r.revision.proper ? " · PROPER" : ""}
              {r.revision.repack ? " · REPACK" : ""}
              {r.revision.real ? ` · REAL×${r.revision.real}` : ""}
            </Fact>
            {r.extension ? <Fact label={t("tester.extension")}>.{r.extension}{r.sample ? ` · ${t("tester.sample")}` : ""}</Fact> : null}
          </dl>
        </Panel>
        <Panel title={t("tester.qualityTitle")}>
          <p className="mb-2 text-[20px] font-semibold tracking-tight text-fg" data-testid="tester-quality">
            {data.quality.name}
          </p>
          <dl>
            <Fact label={t("tester.source")}>{v.source ? t(`tester.sources.${v.source}`, { defaultValue: v.source }) : "–"}{v.uhd ? " · UHD" : ""}</Fact>
            <Fact label={t("tester.resolution")}>{v.resolution ? `${v.resolution}${v.interlaced ? "i" : "p"}` : "–"}</Fact>
            <Fact label={t("tester.codec")}>{[v.codec, v.encoder].filter(Boolean).join(" · ") || "–"}</Fact>
            {v.bitDepth ? <Fact label={t("tester.bitDepth")}>{v.bitDepth}-bit</Fact> : null}
            {v.hdr?.length ? (
              <Fact label="HDR">
                <span className="flex flex-wrap gap-1">
                  {v.hdr.map((h) => <Chip key={h} tone="warning">{h}</Chip>)}
                  {v.dolbyVisionProfile ? <Chip>{t("tester.dvProfile", { profile: v.dolbyVisionProfile })}</Chip> : null}
                </span>
              </Fact>
            ) : null}
            {r.streamingService ? <Fact label={t("tester.streaming")}>{r.streamingService}</Fact> : null}
            <Fact label={t("tester.audio")}>
              {[a.codecs?.join(" + "), a.channels, a.atmos ? "Atmos" : "", a.bitrate, a.bitDepth ? `${a.bitDepth}-bit` : "",
                a.sampleRateKHz ? `${new Intl.NumberFormat(i18n.language).format(a.sampleRateKHz)} kHz` : ""].filter(Boolean).join(" · ") || "–"}
            </Fact>
            <Fact label={t("tester.languages")}>
              {lang(r.languages) || t("tester.untagged")}
              {r.multiAudio ? ` · ${t("tester.multiAudio")}` : ""}
              {r.dualAudio ? ` · ${t("tester.dualAudio")}` : ""}
            </Fact>
            {r.subtitleLanguages?.length || r.multiSubs || r.hardcodedSubs ? (
              <Fact label={t("tester.subtitles")}>
                {[lang(r.subtitleLanguages), r.multiSubs ? t("tester.multiSubs") : "", r.hardcodedSubs ? t("tester.hardcoded") : ""].filter(Boolean).join(" · ")}
              </Fact>
            ) : null}
            {r.flags?.length ? (
              <Fact label={t("tester.flags")}>
                <span className="flex flex-wrap gap-1">{r.flags.map((f) => <Chip key={f}>{f}</Chip>)}</span>
              </Fact>
            ) : null}
          </dl>
        </Panel>
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[2fr_1fr]">
        <Panel title={t("tester.formats")} actions={withProfile ? <span className="text-[12px] text-muted">{t("tester.total")} <Score value={data.score} /></span> : null}>
          {data.customFormats.length === 0 ? <p className="text-[13px] text-faint">{t("tester.noFormats")}</p> : null}
          <ul className="flex flex-col gap-1" data-testid="tester-formats">
            {[...matched, ...others].map((f) => (
              <FormatRow key={f.id} name={f.name} matched={f.matched} score={f.score} conditions={f.conditions} showScore={withProfile} />
            ))}
          </ul>
        </Panel>
        <Panel title={t("tester.understood")}>
          <div className="mb-3">
            <div className="mb-1 flex items-baseline justify-between text-[12px]">
              <span className="text-muted">{t("tester.confidence")}</span>
              <span className="font-mono tabular-nums text-fg" data-testid="tester-confidence">{confidence}%</span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-surface-3" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={confidence}
              aria-label={t("tester.confidence")}>
              <div className={`h-full rounded-full ${confidence >= 80 ? "bg-accent" : confidence >= 50 ? "bg-warning" : "bg-danger"}`}
                style={{ width: `${confidence}%` }} />
            </div>
          </div>
          <p className="mb-1 text-[12px] text-muted">{t("tester.unparsed")}</p>
          {r.unparsed?.length ? (
            <div className="flex flex-wrap gap-1" data-testid="tester-unparsed">
              {r.unparsed.map((u, i) => <Chip key={`${u}-${i}`} tone="warning">{u}</Chip>)}
            </div>
          ) : (
            <p className="text-[13px] text-faint">{t("tester.allUnderstood")}</p>
          )}
        </Panel>
      </div>
    </div>
  );
}

function FormatRow({ name, matched, score, conditions, showScore }: { name: string; matched: boolean; score: number;
  conditions: ParseResult["customFormats"][number]["conditions"]; showScore: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <li className={`rounded-ui border ${matched ? "border-accent/30 bg-success-bg/40" : "border-line"}`}>
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[13px]">
        {matched ? <Check className="size-3.5 shrink-0 text-accent" aria-hidden /> : <X className="size-3.5 shrink-0 text-faint" aria-hidden />}
        <span className={`flex-1 truncate ${matched ? "text-fg" : "text-muted"}`}>{name}</span>
        <span className="sr-only">{matched ? t("tester.matches") : t("tester.noMatch")}</span>
        {showScore && matched ? <Score value={score} /> : null}
        {open ? <ChevronDown className="size-3.5 text-faint" /> : <ChevronRight className="size-3.5 text-faint" />}
      </button>
      {open ? (
        <ul className="border-t border-line px-2.5 py-1.5">
          {conditions.map((c, i) => (
            <li key={i} className="flex items-center gap-2 py-0.5 text-[12px]">
              {c.passed ? <Check className="size-3 text-accent" aria-hidden /> : <X className="size-3 text-danger" aria-hidden />}
              <span className="text-fg">{c.name || c.implementation}</span>
              <span className="text-faint">{t(`formats.impl.${c.implementation}`, { defaultValue: c.implementation })}</span>
              {c.required ? <Chip>{t("formats.required")}</Chip> : null}
              {c.negate ? <Chip>{t("formats.negate")}</Chip> : null}
              {c.unsupported ? <Chip tone="warning">{t("formats.unsupported")}</Chip> : null}
            </li>
          ))}
        </ul>
      ) : null}
    </li>
  );
}

function Verdict({ status, rejections }: { status: "accepted" | "pending" | "rejected"; rejections: Rejection[] }) {
  const { t, i18n } = useTranslation();
  const style = {
    accepted: { cls: "border-accent/40 bg-success-bg", icon: <Check className="size-5 text-accent" aria-hidden />, text: "text-accent" },
    pending: { cls: "border-warning/40 bg-warning-bg", icon: <Clock3 className="size-5 text-warning" aria-hidden />, text: "text-warning" },
    rejected: { cls: "border-danger/40 bg-danger-bg", icon: <CircleAlert className="size-5 text-danger" aria-hidden />, text: "text-danger" },
  }[status];
  return (
    <section className={`rounded-ui border px-4 py-3 ${style.cls}`} data-testid="tester-verdict" aria-live="polite">
      <div className="flex items-center gap-2">
        {style.icon}
        <h2 className={`text-[15px] font-semibold ${style.text}`}>{t(`tester.verdict.${status}`)}</h2>
      </div>
      {rejections.length ? (
        <ul className="mt-2 flex flex-col gap-1.5" data-testid="tester-rejections">
          {rejections.map((r, i) => (
            <li key={i} className="flex flex-wrap items-baseline gap-2 text-[13px]">
              <Chip tone={r.temporary ? "warning" : "danger"}>{r.code}</Chip>
              <span className="text-fg">{describeRejection(t, r, i18n.language)}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-1 text-[13px] text-muted">{t("tester.verdict.acceptedHint")}</p>
      )}
    </section>
  );
}
