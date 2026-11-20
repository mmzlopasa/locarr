import { Component, type ErrorInfo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouter, type ErrorComponentProps } from "@tanstack/react-router";
import { errorMessage } from "@/components/errors";
import { Button } from "@/components/ui";
import i18n from "@/i18n";

/** Shown when a page fails to load or render; the rest of the app keeps working. */
export function RouteErrorPage({ error, reset }: ErrorComponentProps) {
  const { t } = useTranslation();
  const router = useRouter();
  return (
    <div role="alert" className="mx-auto mt-16 max-w-md rounded-ui border border-line bg-surface p-6 text-center">
      <h1 className="text-[16px] font-semibold text-fg">{t("errorPage.title")}</h1>
      <p className="mt-2 text-[13px] text-muted">{errorMessage(t, error)}</p>
      <div className="mt-4 flex justify-center gap-2">
        <Button
          variant="primary"
          onClick={() => {
            reset();
            void router.invalidate();
          }}
        >
          {t("errorPage.retry")}
        </Button>
        <Button onClick={() => window.location.assign("/")}>{t("errorPage.home")}</Button>
      </div>
    </div>
  );
}

/** Last line of defence around the whole app (errors outside any route). */
export class RootErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error("unhandled UI error", error, info.componentStack);
  }

  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <div role="alert" className="mx-auto mt-16 max-w-md rounded-ui border border-line bg-surface p-6 text-center">
        <h1 className="text-[16px] font-semibold text-fg">{i18n.t("errorPage.title")}</h1>
        <p className="mt-2 text-[13px] text-muted">{i18n.t("errorPage.reload")}</p>
        <div className="mt-4">
          <Button variant="primary" onClick={() => window.location.reload()}>
            {i18n.t("errorPage.reloadButton")}
          </Button>
        </div>
      </div>
    );
  }
}
