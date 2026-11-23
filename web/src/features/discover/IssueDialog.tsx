import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type IssueInput, type IssueType, type TitleDetail } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Alert, Button, ErrorAlert, SelectInput, TextInput } from "@/components/ui";

export const ISSUE_TYPES: IssueType[] = ["video", "audio", "subtitles", "other"];

/** Report a problem with an available title. */
export function IssueDialog({ detail, kind, target, onClose }: {
  detail: TitleDetail; kind: "movie" | "series" | "album"; target?: { mbid: string; title: string }; onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [type, setType] = useState<IssueType>("video");
  const [season, setSeason] = useState("");
  const [episode, setEpisode] = useState("");
  const [message, setMessage] = useState("");
  const send = useMutation({
    mutationFn: async () => {
      const body: IssueInput = { kind, type, message: message.trim() };
      if (kind === "album") body.mbid = target?.mbid ?? detail.item.mbid;
      else body.tmdbId = detail.item.tmdbId;
      if (kind === "series" && season !== "") body.seasonNumber = Number(season);
      if (kind === "series" && episode !== "") body.episodeNumber = Number(episode);
      return unwrap(await api.POST("/issues", { body }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.issues }),
  });
  return (
    <Modal title={t("issues.reportTitle", { title: target?.title ?? detail.item.title })} onClose={onClose} testId="issue-dialog"
      footer={send.isSuccess ? (
        <Button variant="primary" onClick={onClose}>{t("common.done")}</Button>
      ) : (
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" busy={send.isPending} disabled={!message.trim()} onClick={() => send.mutate()} data-testid="issue-send">
            {t("issues.send")}
          </Button>
        </>
      )}>
      {send.isSuccess ? (
        <Alert tone="success">{t("issues.sent")}</Alert>
      ) : (
        <div className="flex flex-col gap-3">
          <SelectInput label={t("issues.type")} value={type} onChange={(e) => setType(e.target.value as IssueType)} data-testid="issue-type">
            {ISSUE_TYPES.map((it) => <option key={it} value={it}>{t(`issues.types.${it}`)}</option>)}
          </SelectInput>
          {kind === "series" ? (
            <div className="grid grid-cols-2 gap-3">
              <TextInput label={t("issues.season")} inputMode="numeric" value={season} onChange={(e) => setSeason(e.target.value.replace(/\D/g, ""))}
                hint={t("issues.optional")} />
              <TextInput label={t("issues.episode")} inputMode="numeric" value={episode} onChange={(e) => setEpisode(e.target.value.replace(/\D/g, ""))}
                hint={t("issues.optional")} />
            </div>
          ) : null}
          <div className="flex flex-col gap-1">
            <label htmlFor="issue-message" className="text-[12px] font-medium text-muted">{t("issues.message")}</label>
            <textarea id="issue-message" rows={3} maxLength={2000} value={message} onChange={(e) => setMessage(e.target.value)}
              placeholder={t("issues.messageHint")} data-testid="issue-message"
              className="w-full rounded-ui border border-line-strong bg-surface-2 px-2.5 py-1.5 text-[13px] text-fg placeholder:text-faint focus:border-info focus:outline-none" />
          </div>
          <ErrorAlert error={send.error} />
        </div>
      )}
    </Modal>
  );
}
