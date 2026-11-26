import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { formatBytes } from "@/components/format";
import { Modal } from "@/components/Modal";
import { Button, ErrorAlert, SelectInput, Spinner, TextInput } from "@/components/ui";
import { useQualityProfiles } from "@/features/movies/common";
import {
  galleryImagePath, useAddGallery, useAdultPerformers, useAdultRootFolders, useAdultStudios, useGalleryImages,
  type AdultGalleryImage,
} from "./api";
import { BlurImage, useAdultImage } from "./BlurImage";
import { EmptyAdult } from "./components";

/** A gallery's thumbnails; a click opens the lightbox (after revealing, when images are blurred). */
export function GalleryGrid({ id, title, hasFile }: { id: number; title: string; hasFile: boolean }) {
  const { t, i18n } = useTranslation();
  const images = useGalleryImages(id, hasFile);
  const [open, setOpen] = useState<number | null>(null);
  if (!hasFile) return <EmptyAdult title={t("adult.gallery.noImages")} hint={t("adult.gallery.noImagesHint")} />;
  if (images.isPending) return <Spinner />;
  if (!images.data) return <ErrorAlert error={images.error} />;
  const list = images.data;
  if (list.length === 0) return <EmptyAdult title={t("adult.gallery.noImages")} hint={t("adult.gallery.missingHint")} />;
  return (
    <div data-testid="adult-gallery-grid">
      <ul className="grid grid-cols-3 gap-1.5 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-8">
        {list.map((im) => (
          <li key={im.index} className="min-w-0">
            <BlurImage image={galleryImagePath(id, im.index, true)} alt={t("adult.gallery.imageAlt", { title, n: im.index })}
              fallback={String(im.index)} className="aspect-[3/4] w-full" onOpen={() => setOpen(im.index)} />
            <span className="sr-only">{im.name}, {formatBytes(im.size, i18n.language)}</span>
          </li>
        ))}
      </ul>
      {open !== null ? <Lightbox id={id} title={title} images={list} index={open} onIndex={setOpen} onClose={() => setOpen(null)} /> : null}
    </div>
  );
}

/** Full-size viewer: arrows and ← → keys move, Escape closes. */
function Lightbox({ id, title, images, index, onIndex, onClose }:
  { id: number; title: string; images: AdultGalleryImage[]; index: number; onIndex: (i: number) => void; onClose: () => void }) {
  const { t } = useTranslation();
  const pos = images.findIndex((im) => im.index === index);
  const { src, loading } = useAdultImage(galleryImagePath(id, index));
  const closeRef = useRef<HTMLButtonElement>(null);
  const go = useCallback((d: number) => {
    const next = images[(pos + d + images.length) % images.length];
    if (next) onIndex(next.index);
  }, [images, pos, onIndex]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowRight") go(1);
      else if (e.key === "ArrowLeft") go(-1);
    };
    window.addEventListener("keydown", onKey);
    closeRef.current?.focus();
    return () => window.removeEventListener("keydown", onKey);
  }, [go, onClose]);
  // Opened only from a revealed (or unblurred) thumbnail, so never blurred here.
  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-black/90" role="dialog" aria-modal="true" aria-label={title} data-testid="adult-lightbox">
      <div className="flex h-12 shrink-0 items-center justify-between gap-2 px-3 text-[13px] text-fg/85">
        <span className="tabular-nums" data-testid="adult-lightbox-pos">{t("adult.gallery.position", { n: pos + 1, total: images.length })}</span>
        <button ref={closeRef} type="button" onClick={onClose} className="rounded-ui p-1.5 hover:bg-white/10" aria-label={t("common.close")}>
          <X className="size-5" aria-hidden />
        </button>
      </div>
      <div className="relative flex min-h-0 flex-1 items-center justify-center px-2 pb-4 sm:px-14">
        {loading ? <Spinner /> : src ? (
          <img src={src} alt={t("adult.gallery.imageAlt", { title, n: pos + 1 })} className="max-h-full max-w-full select-none object-contain" draggable={false} />
        ) : <p className="text-[13px] text-fg/70">{t("adult.image.unavailable", { name: title })}</p>}
        {images.length > 1 ? (
          <>
            <button type="button" onClick={() => go(-1)} aria-label={t("adult.gallery.previous")}
              className="absolute left-1 top-1/2 -translate-y-1/2 rounded-full bg-black/40 p-2 text-fg hover:bg-black/60 sm:left-3">
              <ChevronLeft className="size-5" aria-hidden />
            </button>
            <button type="button" onClick={() => go(1)} aria-label={t("adult.gallery.next")} data-testid="adult-lightbox-next"
              className="absolute right-1 top-1/2 -translate-y-1/2 rounded-full bg-black/40 p-2 text-fg hover:bg-black/60 sm:right-3">
              <ChevronRight className="size-5" aria-hidden />
            </button>
          </>
        ) : null}
      </div>
    </div>
  );
}

/**
 * A new gallery typed in by hand. Neither StashDB nor ThePornDB lists image
 * sets, so title, studio, date and performers come from the person (or from
 * a scene or performer page, which use their own buttons).
 */
export function NewGalleryForm({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const studios = useAdultStudios();
  const perfs = useAdultPerformers();
  const roots = useAdultRootFolders();
  const profiles = useQualityProfiles();
  const add = useAddGallery();
  const [title, setTitle] = useState("");
  const [studioId, setStudioId] = useState(0);
  const [studio, setStudio] = useState("");
  const [date, setDate] = useState("");
  const [performerIds, setPerformerIds] = useState<number[]>([]);
  const [profileId, setProfileId] = useState(0);
  const [rootId, setRootId] = useState(0);
  const submit = () => add.mutate({
    title: title.trim(), ...(studioId ? { studioId } : studio.trim() ? { studio: studio.trim() } : {}), ...(date ? { releaseDate: date } : {}),
    performerIds, ...(profileId ? { qualityProfileId: profileId } : {}), ...(rootId ? { rootFolderId: rootId } : {}), monitored: true, search: true,
  }, { onSuccess: (it) => void navigate({ to: "/adult/items/$id", params: { id: String(it.id) } }) });
  return (
    <Modal title={t("adult.gallery.newTitle")} onClose={onClose} testId="adult-gallery-new" wide
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" busy={add.isPending} disabled={!title.trim()} onClick={submit} data-testid="adult-gallery-create">{t("adult.gallery.create")}</Button>
        </>
      }>
      <form className="grid gap-3 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); if (title.trim()) submit(); }}>
        <p className="text-[12px] text-muted sm:col-span-2">{t("adult.gallery.newHint")}</p>
        <div className="sm:col-span-2"><TextInput label={t("adult.gallery.title")} value={title} maxLength={300} onChange={(e) => setTitle(e.target.value)} data-testid="adult-gallery-title" /></div>
        <SelectInput label={t("adult.gallery.studio")} value={studioId} onChange={(e) => setStudioId(Number(e.target.value))}>
          <option value={0}>{t("adult.gallery.noStudio")}</option>
          {studios.data?.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </SelectInput>
        {studioId ? <span className="hidden sm:block" /> : (
          <TextInput label={t("adult.gallery.studioName")} value={studio} maxLength={200} onChange={(e) => setStudio(e.target.value)} />
        )}
        <TextInput label={t("adult.gallery.date")} type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        <SelectInput label={t("adult.gallery.performers")} multiple value={performerIds.map(String)} className="min-h-24"
          onChange={(e) => setPerformerIds([...e.target.selectedOptions].map((o) => Number(o.value)))}>
          {perfs.data?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("adult.add.profile")} value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
          <option value={0}>{t("adult.add.default")}</option>
          {(profiles.data ?? []).filter((p) => p.kind === "movies").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </SelectInput>
        <SelectInput label={t("adult.add.root")} value={rootId} onChange={(e) => setRootId(Number(e.target.value))}>
          <option value={0}>{t("adult.add.default")}</option>
          {roots.data?.map((r) => <option key={r.id} value={r.id}>{r.path}</option>)}
        </SelectInput>
      </form>
      <div className="mt-3"><ErrorAlert error={add.error} /></div>
    </Modal>
  );
}
