#!/usr/bin/env python3
"""Builds the iOS string catalog (ios/Locarr/Resources/Localizable.xcstrings).

Sources:
  - the web client's locales (web/src/i18n/locales/{en,hr}.json) for the namespaces the app
    shares with the web (error codes, "why isn't it here?", board, health, timeline …), so
    server answers read the same in both clients;
  - ios/strings/app.json for the app's own strings: {"key": ["English", "Hrvatski"]}.

Keys are flattened with dots ("explain.codes.downloading"); placeholders stay as {{name}}
(filled by Loc.render). Plural forms keep the web's suffixes (_one, _few, _other).

  scripts/ios-strings.py          write the catalog
  scripts/ios-strings.py --check  fail when the catalog is stale, a key used in Swift is
                                  missing, or a Croatian translation is missing
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
WEB = ROOT / "web/src/i18n/locales"
APP = ROOT / "ios/strings/app.json"
OUT = ROOT / "ios/Locarr/Resources/Localizable.xcstrings"
SWIFT = ROOT / "ios/Locarr"

# Whole namespaces shared with the web client; "music.types" takes only that subtree.
SHARED = [
    "common", "errors", "nav", "roles", "login", "mediaKinds", "downloadStates", "availability", "transfer",
    "explain", "rejectionCodes", "timeline", "board", "health", "history", "overview", "queue", "interactive",
    "wanted", "calendar", "indexers", "discover", "title", "request", "requests", "comments", "issues", "notifications",
    "music.types",
]


def flatten(tree, prefix=""):
    out = {}
    for k, v in tree.items():
        key = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict):
            out.update(flatten(v, key))
        else:
            out[key] = str(v)
    return out


def subtree(tree, path):
    for part in path.split("."):
        tree = tree.get(part, {})
    return tree


def build():
    en_web = json.loads((WEB / "en.json").read_text(encoding="utf-8"))
    hr_web = json.loads((WEB / "hr.json").read_text(encoding="utf-8"))
    entries = {}
    problems = []
    for ns in SHARED:
        en = flatten(subtree(en_web, ns), ns)
        hr = flatten(subtree(hr_web, ns), ns)
        for key in sorted(set(en) | set(hr)):
            e = en.get(key)
            h = hr.get(key)
            if e is None:
                # Croatian-only plural forms (_few): English never asks for them.
                base = re.sub(r"_(few|many)$", "_other", key)
                e = en.get(base, h)
            if h is None:
                problems.append(f"web locale hr.json has no {key}")
                h = e
            entries[key] = (e, h)
    app = json.loads(APP.read_text(encoding="utf-8"))
    for key, (e, h) in app.items():
        if key in entries and entries[key] != (e, h):
            problems.append(f"{key} is defined by the web locales and ios/strings/app.json")
        if not h.strip():
            problems.append(f"{key} has no Croatian text")
        entries[key] = (e, h)

    strings = {}
    for key in sorted(entries):
        e, h = entries[key]
        strings[key] = {
            "extractionState": "manual",
            "localizations": {
                "en": {"stringUnit": {"state": "translated", "value": e}},
                "hr": {"stringUnit": {"state": "translated", "value": h}},
            },
        }
    catalog = {"sourceLanguage": "en", "strings": strings, "version": "1.0"}
    return json.dumps(catalog, indent=2, ensure_ascii=False, sort_keys=True) + "\n", entries, problems


# Literal keys in Swift: L("…"), Loc.render("…"), Loc.has("…"), Loc.string("…") and the
# string literals in ternaries inside L(…).
KEY_LITERAL = re.compile(r'"([a-z][A-Za-z0-9]*\.[A-Za-z0-9_.]*(?:\\\([^"]*\)[A-Za-z0-9_.]*)*)"')
NOT_KEYS = re.compile(r"^(net\.scyde|locarr\.|home\.arpa|ts\.net)")


def used_keys():
    keys = set()
    for f in SWIFT.rglob("*.swift"):
        text = f.read_text(encoding="utf-8")
        for line in text.splitlines():
            for m in KEY_LITERAL.finditer(line):
                k = m.group(1)
                before = line[:m.start()].rstrip()
                if before.endswith(("systemName:", "icon:", "systemImage:")):
                    continue  # SF Symbol names look like keys
                if NOT_KEYS.match(k):
                    continue
                # SF Symbol names look like keys; only check strings that reach Loc.
                if not re.search(r'(\bL\(|Loc\.(render|has|string|lookup)\(|key: ")', line):
                    continue
                keys.add(k)
    return keys


def main():
    text, entries, problems = build()
    if "--check" not in sys.argv:
        OUT.write_text(text, encoding="utf-8")
        print(f"wrote {OUT.relative_to(ROOT)} ({len(entries)} strings)")
        for p in problems:
            print("warning:", p)
        return
    errors = list(problems)
    if not OUT.exists() or OUT.read_text(encoding="utf-8") != text:
        errors.append(f"{OUT.relative_to(ROOT)} is out of date: run scripts/ios-strings.py")
    for k in sorted(used_keys()):
        if "\\(" in k:
            prefix = k.split("\\(")[0]
            if not any(e.startswith(prefix) for e in entries):
                errors.append(f"no strings start with {prefix} (used as {k})")
        elif k not in entries and not any(e.startswith(k + "_") for e in entries):
            errors.append(f"missing string {k}")
    for e in errors:
        print("error:", e)
    if errors:
        sys.exit(1)
    print(f"ok: {len(entries)} strings")


if __name__ == "__main__":
    main()
