import { useEffect, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { EyeOff } from "lucide-react";
import { useAdultPreferences } from "./api";
import { initial } from "./logic";

export type AdultLink =
  | { to: "/adult/items/$id"; params: { id: string } }
  | { to: "/adult/studios/$id"; params: { id: string } }
  | { to: "/adult/performers/$id"; params: { id: string } };

const keyPattern = /^[0-9a-f]{32}$/;

const galleryPath = /^\/api\/v1\/adult\/items\/\d+\/images\/\d+(\?thumb=true)?$/;

/** The address of an image key or a gallery image path (both private). */
function imageURL(image: string | undefined): string | null {
  if (!image) return null;
  if (keyPattern.test(image)) return `/api/v1/adult/images/${image}`;
  return galleryPath.test(image) ? image : null;
}

/** Loads a private image with the session (never a public URL). */
export function useAdultImage(image: string | undefined) {
  const [state, setState] = useState<{ key?: string; src: string | null; failed: boolean }>({ src: null, failed: false });
  useEffect(() => {
    const address = imageURL(image);
    if (!address) return;
    let url: string | null = null;
    let cancelled = false;
    fetch(address, { credentials: "same-origin" })
      .then(async (r) => {
        if (cancelled) return;
        if (!r.ok) {
          setState({ key: image, src: null, failed: true });
          return;
        }
        url = URL.createObjectURL(await r.blob());
        if (cancelled) URL.revokeObjectURL(url);
        else setState({ key: image, src: url, failed: false });
      })
      .catch(() => {
        if (!cancelled) setState({ key: image, src: null, failed: true });
      });
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [image]);
  const valid = imageURL(image) !== null;
  if (!valid) return { src: null, unavailable: true, loading: false };
  const current = state.key === image;
  return { src: current ? state.src : null, unavailable: current && state.failed, loading: !current };
}

/**
 * An adult image (a cache key, or a gallery image path). Blurred until clicked when the viewer's preference says so
 * (the default); the eye-slash marks only a blurred image. Without an image
 * (none at the source, or it could not be fetched) a neutral placeholder
 * shows the initial of `fallback`. Revealed or unblurred, it links to `link`.
 */
export function BlurImage({ image, alt, fallback, className = "", fit = "cover", link, onOpen, children }:
  { image?: string; alt: string; fallback?: string; className?: string; fit?: "cover" | "contain"; link?: AdultLink;
    /** Opens the image (a lightbox) once it is not blurred. */
    onOpen?: () => void; children?: ReactNode }) {
  const { t } = useTranslation();
  const prefs = useAdultPreferences();
  const blur = prefs.data?.blur ?? true;
  const [revealed, setRevealed] = useState(false);
  const { src, unavailable, loading } = useAdultImage(image);
  const box = `relative block overflow-hidden rounded-ui border border-line bg-surface-2 ${className}`;

  if (!src) {
    const inner = (
      <>
        {loading ? <span className="absolute inset-0 animate-pulse bg-surface-3" aria-hidden /> : (
          <span className="absolute inset-0 flex items-center justify-center" aria-hidden>
            <span className="flex size-[clamp(28px,38%,56px)] items-center justify-center rounded-full border border-line-strong bg-surface-3 text-[15px] font-semibold text-muted">
              {initial(fallback ?? alt)}
            </span>
          </span>
        )}
        {children}
      </>
    );
    const label = unavailable ? t("adult.image.unavailable", { name: alt }) : alt;
    const state = loading ? "loading" : "placeholder";
    if (onOpen && !loading) {
      return <button type="button" className={box} aria-label={label} data-testid="adult-image" data-state={state} onClick={onOpen}>{inner}</button>;
    }
    return link ? (
      // Every image link sits next to a text link to the same page: keep it out of the tab order and the accessibility tree.
      <Link {...link} className={box} tabIndex={-1} aria-hidden data-testid="adult-image" data-state={state}>{inner}</Link>
    ) : (
      <span role="img" className={box} aria-label={label} data-testid="adult-image" data-state={state}>{inner}</span>
    );
  }

  const hidden = blur && !revealed;
  const img = (
    <img src={src} alt={hidden ? "" : alt} draggable={false}
      className={`absolute inset-0 size-full ${fit === "contain" ? "object-contain p-2" : "object-cover"} transition-[filter,transform] duration-200 ${hidden ? "scale-110 blur-xl" : ""}`} />
  );
  if (hidden) {
    return (
      <button type="button" className={`${box} text-left`} data-testid="adult-image" data-blurred="true" data-state="image"
        onClick={(e) => { e.preventDefault(); e.stopPropagation(); setRevealed(true); }} aria-label={t("adult.image.reveal")}>
        {img}
        <span className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-black/25 text-[11px] font-medium text-fg/85">
          <EyeOff className="size-4" aria-hidden />
          {t("adult.image.tap")}
        </span>
        {children}
      </button>
    );
  }
  if (link) {
    return (
      <Link {...link} className={box} tabIndex={-1} aria-hidden data-testid="adult-image" data-blurred="false" data-state="image">
        {img}
        {children}
      </Link>
    );
  }
  if (onOpen) {
    return (
      <button type="button" className={box} data-testid="adult-image" data-blurred="false" data-state="image" onClick={onOpen} aria-label={alt}>
        {img}
        {children}
      </button>
    );
  }
  return (
    <button type="button" className={box} data-testid="adult-image" data-blurred="false" data-state="image"
      onClick={() => blur && setRevealed(false)} aria-label={blur ? t("adult.image.hide") : alt}>
      {img}
      {children}
    </button>
  );
}
