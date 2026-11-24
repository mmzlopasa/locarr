import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { EyeOff } from "lucide-react";
import { useAdultPreferences } from "./api";

/**
 * An adult image: fetched with the session (never a public URL), blurred
 * until clicked unless the viewer turned blurring off. Without an image key a
 * neutral placeholder is shown.
 */
export function BlurImage({ image, alt, className = "" }: { image?: string; alt: string; className?: string }) {
  const { t } = useTranslation();
  const prefs = useAdultPreferences();
  const blur = prefs.data?.blur ?? true;
  const [revealed, setRevealed] = useState(false);
  const [src, setSrc] = useState<string | null>(null);
  useEffect(() => {
    if (!image || !/^[0-9a-f]{32}$/.test(image)) return;
    let url: string | null = null;
    let cancelled = false;
    void fetch(`/api/v1/adult/images/${image}`, { credentials: "same-origin" }).then(async (r) => {
      if (!r.ok || cancelled) return;
      url = URL.createObjectURL(await r.blob());
      setSrc(url);
    });
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [image]);
  const hidden = blur && !revealed;
  return (
    <button type="button" data-testid="adult-image" data-blurred={hidden ? "true" : "false"}
      className={`relative block overflow-hidden rounded-ui bg-surface-3 ${className}`}
      onClick={() => setRevealed((v) => !v)} aria-label={hidden ? t("adult.image.reveal") : t("adult.image.hide")}>
      {src ? (
        <img src={src} alt={alt} className={`size-full object-cover transition-[filter] duration-200 ${hidden ? "scale-110 blur-xl" : ""}`} />
      ) : (
        <span className="flex size-full items-center justify-center text-faint" aria-hidden><EyeOff className="size-5" /></span>
      )}
      {hidden && src ? (
        <span className="absolute inset-0 flex items-center justify-center text-[11px] font-medium text-fg/80">{t("adult.image.tap")}</span>
      ) : null}
    </button>
  );
}
