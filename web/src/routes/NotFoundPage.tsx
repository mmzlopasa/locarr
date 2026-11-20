import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <div className="mx-auto mt-16 max-w-md rounded-ui border border-line bg-surface p-6 text-center">
      <p className="font-mono text-[12px] text-faint">404</p>
      <h1 className="mt-1 text-[16px] font-semibold text-fg">{t("notFound.title")}</h1>
      <p className="mt-2 text-[13px] text-muted">{t("notFound.body")}</p>
      <Link to="/" className="mt-4 inline-block text-[13px] text-accent hover:underline">
        {t("notFound.home")}
      </Link>
    </div>
  );
}
