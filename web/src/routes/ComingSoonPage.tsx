import { useTranslation } from "react-i18next";
import { Badge, PageHeader } from "@/components/ui";

/**
 * A section that a later phase of the plan builds. It says so plainly instead of
 * pretending: no fake data, no dead buttons.
 */
export function ComingSoonPage({ section, phase }: { section: string; phase: string }) {
  const { t } = useTranslation();
  return (
    <div>
      <PageHeader title={t(`nav.${section}`)} actions={<Badge tone="info">{t("comingSoon.badge")}</Badge>} />
      <div className="rounded-ui border border-dashed border-line-strong bg-surface p-6" data-testid="coming-soon">
        <p className="text-[14px] font-medium text-fg">{t("comingSoon.title", { phase })}</p>
        <p className="mt-1 max-w-prose text-[13px] text-muted">{t(`comingSoon.sections.${section}`)}</p>
        <p className="mt-3 text-[12px] text-faint">{t("comingSoon.now")}</p>
      </div>
    </div>
  );
}
