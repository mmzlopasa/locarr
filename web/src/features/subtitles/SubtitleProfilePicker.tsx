import { useTranslation } from "react-i18next";
import { ProfileOptions } from "./SubtitleSettingsPage";
import { useSubtitleItem, useSubtitleProfiles } from "./common";

/**
 * The subtitle profile select of a movie or series edit form. NaN keeps the
 * current setting; 0 = the kind's default, -1 = no subtitles. With an itemRef
 * the current choice is shown.
 */
export function SubtitleProfilePicker({ value, onChange, itemRef }: { value: number; onChange: (v: number) => void; itemRef?: string }) {
  const { t } = useTranslation();
  const profiles = useSubtitleProfiles();
  return (
    <div>
      <ProfileOptions label={t("subtitles.picker.label")} value={Number.isNaN(value) ? "" : value} onChange={onChange} profiles={profiles.data ?? []} keep />
      {itemRef ? <Current itemRef={itemRef} /> : null}
    </div>
  );
}

function Current({ itemRef }: { itemRef: string }) {
  const { t } = useTranslation();
  const item = useSubtitleItem(itemRef);
  const it = item.data;
  if (!it) return null;
  const text = it.profile ? t(`subtitles.profileFrom.${it.profileSource}`, { name: it.profile.name })
    : it.profileOverride === -1 ? t("subtitles.profileOff") : t("subtitles.profileNone");
  return <p className="mt-1 text-[11px] text-faint">{text}</p>;
}
