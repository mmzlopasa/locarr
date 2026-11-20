import en from "./locales/en.json";
import hr from "./locales/hr.json";

function keys(obj: object, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === "object" ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

function values(obj: object): string[] {
  return Object.values(obj).flatMap((v) => (v && typeof v === "object" ? values(v) : [String(v)]));
}

describe("i18n", () => {
  it("HR and EN define exactly the same keys", () => {
    expect(keys(hr).sort()).toEqual(keys(en).sort());
  });

  it("has no empty strings", () => {
    for (const dict of [en, hr]) {
      for (const v of values(dict)) expect(v.trim()).not.toBe("");
    }
  });

  it("uses the same interpolation placeholders in both languages", () => {
    const flat = (obj: object, prefix = ""): [string, string][] =>
      Object.entries(obj).flatMap(([k, v]) =>
        v && typeof v === "object" ? flat(v, `${prefix}${k}.`) : [[`${prefix}${k}`, String(v)] as [string, string]],
      );
    const hrMap = new Map(flat(hr));
    for (const [k, v] of flat(en)) {
      const vars = (s: string) => (s.match(/{{\s*\w+\s*}}/g) ?? []).sort();
      expect(vars(hrMap.get(k) ?? ""), k).toEqual(vars(v));
    }
  });

  it("translates every API error code the server documents", () => {
    const codes = ["invalid_credentials", "totp_required", "invalid_totp", "account_locked", "rate_limited", "setup_done",
      "bad_setup_code", "username_taken", "invalid_username", "weak_password", "last_admin", "self_action", "not_found",
      "forbidden", "unauthorized", "session_required", "validation", "starting", "request_too_large", "internal",
      "totp_already_on", "totp_not_started", "path_not_allowed", "path_not_found", "already_exists", "unknown_task"];
    for (const c of codes) expect(Object.keys(en.errors)).toContain(c);
  });
});
