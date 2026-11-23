#!/usr/bin/env python3
"""Adds a release to the Feather (AltStore-format) source, newest first (used by ios.yml).

  scripts/update-feather-source.py feather/source.json --version 1.2.3 \
      --url https://github.com/…/Locarr-v1.2.3.ipa --size 12345 [--bundle-id net.scyde.locarr]
"""
import argparse
import datetime
import json


def build_number(version):
    """CFBundleVersion of a release: major*10000 + minor*100 + patch (same formula as ios.yml)."""
    core = version.removeprefix("v").split("-", 1)[0]
    major, minor, patch = (int(x) for x in core.split("."))
    return str(major * 10000 + minor * 100 + patch)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("source")
    p.add_argument("--version", required=True)
    p.add_argument("--url", required=True)
    p.add_argument("--size", type=int, required=True)
    p.add_argument("--bundle-id", default="net.scyde.locarr")
    a = p.parse_args()
    version = a.version.removeprefix("v")

    with open(a.source, encoding="utf-8") as f:
        src = json.load(f)
    app = next((x for x in src["apps"] if x["bundleIdentifier"] == a.bundle_id), None)
    if app is None:
        raise SystemExit(f"no app with bundleIdentifier {a.bundle_id} in {a.source}")
    app["versions"] = [v for v in app["versions"] if v["version"] != version]
    app["versions"].insert(0, {
        "version": version,
        "buildVersion": build_number(version),
        "date": datetime.date.today().isoformat(),
        "downloadURL": a.url,
        "size": a.size,
        "localizedDescription": f"{app['name']} {version}",
        "minOSVersion": "17.0",
    })
    # Older AltStore clients read these top-level fields of the newest version.
    app["version"] = version
    app["versionDate"] = app["versions"][0]["date"]
    app["downloadURL"] = a.url
    app["size"] = a.size
    with open(a.source, "w", encoding="utf-8") as f:
        json.dump(src, f, indent=2, ensure_ascii=False)
        f.write("\n")


if __name__ == "__main__":
    main()
