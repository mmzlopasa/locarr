import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, queryKeys, unwrap, type User } from "@/api/client";
import { Badge, Button, ErrorAlert, SelectInput } from "@/components/ui";

/** Streamero's accounts (admins); fails quietly when Streamero is not set up. */
export function useStreameroUsers() {
  return useQuery({
    queryKey: queryKeys.streameroUsers,
    queryFn: async () => unwrap(await api.GET("/mediaserver/streamero/users")),
    retry: false,
    staleTime: 60_000,
  });
}

/** Links a Locarr user to a Streamero account so they can sign in with it. */
export function StreameroLink({ user }: { user: User }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const accounts = useStreameroUsers();
  const [subject, setSubject] = useState("");
  const path = { params: { path: { id: user.id, provider: "streamero" } } };
  const done = () => qc.invalidateQueries({ queryKey: queryKeys.streameroUsers });
  const link = useMutation({
    mutationFn: async () => {
      const acc = accounts.data?.find((a) => a.id === subject);
      return unwrap(await api.PUT("/users/{id}/identities/{provider}", { ...path, body: { subject, username: acc?.username } }));
    },
    onSuccess: () => {
      setSubject("");
      return done();
    },
  });
  const unlink = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/users/{id}/identities/{provider}", path)),
    onSuccess: done,
  });
  if (!accounts.data || accounts.data.length === 0) return null;
  const current = accounts.data.find((a) => a.linkedUserId === user.id);
  return (
    <div className="flex flex-col gap-2 border-t border-line pt-3" data-testid={`streamero-link-${user.username}`}>
      <p className="text-[12px] font-medium text-muted">{t("users.streamero.title")}</p>
      {current ? (
        <div className="flex flex-wrap items-center gap-2 text-[13px]">
          <Badge tone="info">Streamero</Badge>
          <span className="text-fg">{current.username}</span>
          <Button variant="ghost" busy={unlink.isPending} onClick={() => unlink.mutate()} data-testid="streamero-unlink">
            {t("users.streamero.unlink")}
          </Button>
        </div>
      ) : (
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-56">
            <SelectInput label={t("users.streamero.account")} value={subject} onChange={(e) => setSubject(e.target.value)}
              data-testid="streamero-account">
              <option value="">{t("users.streamero.choose")}</option>
              {accounts.data.map((a) => (
                <option key={a.id} value={a.id} disabled={!!a.linkedUserId}>
                  {a.username}
                  {a.linkedUsername ? ` (${t("users.streamero.linkedTo", { username: a.linkedUsername })})` : ""}
                  {a.disabled ? ` (${t("users.disabled")})` : ""}
                </option>
              ))}
            </SelectInput>
          </div>
          <Button busy={link.isPending} disabled={!subject} onClick={() => link.mutate()} data-testid="streamero-link">
            {t("users.streamero.link")}
          </Button>
        </div>
      )}
      <p className="text-[12px] text-faint">{t("users.streamero.hint")}</p>
      <ErrorAlert error={link.error ?? unlink.error} />
    </div>
  );
}
