#!/usr/bin/env python3
"""Import Fresh's translations for the gvim i18n catalogue.

The gvim English catalogue (i18n/locales/en.json) is the source of truth. This tool
fills the translated locale files (i18n/locales/<locale>.json) from Fresh's locale
JSON (refs/fresh/crates/fresh-editor-core/locales), using the explicit mapping in
i18n/fresh_map.json (gvim key -> Fresh key). Only keys whose English wording/meaning
matches Fresh are mapped; gvim-only keys stay absent from the translated files and
fall back to English at runtime.

Usage:
  python3 tools/i18n_import.py           # rewrite the translated locale files
  python3 tools/i18n_import.py --check   # exit 1 if the committed files are stale
"""
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
LOCALES = ROOT / "i18n" / "locales"
FRESH = ROOT / "refs" / "fresh" / "crates" / "fresh-editor-core" / "locales"
MAP = ROOT / "i18n" / "fresh_map.json"

# The translated locales: every language gvim offers except English. Fresh ships the
# same set (i18n/i18n.go Locales()); the tool fails if the Fresh checkout differs.
TRANSLATED = ["bg", "cs", "de", "es", "fr", "it", "ja", "ko", "pt-BR", "ru", "th", "uk", "vi", "zh-CN"]


def load(path):
    with path.open(encoding="utf-8") as f:
        return json.load(f)


def placeholders(text):
    out, i = [], 0
    while True:
        start = text.find("%{", i)
        if start < 0:
            return out
        end = text.find("}", start + 2)
        if end < 0:
            raise SystemExit(f"unterminated placeholder in {text!r}")
        out.append(text[start + 2 : end])
        i = end + 1


def main():
    check = "--check" in sys.argv[1:]
    en = load(LOCALES / "en.json")
    mapping = load(MAP)
    unknown = [k for k in mapping if k not in en]
    if unknown:
        raise SystemExit("fresh_map.json names keys not in the English catalogue: " + ", ".join(unknown))
    fresh_en = load(FRESH / "en.json")
    missing_fresh = [f"{g}->{f}" for g, f in mapping.items() if f not in fresh_en]
    if missing_fresh:
        raise SystemExit("fresh_map.json names Fresh keys that do not exist: " + ", ".join(missing_fresh))

    stale = 0
    for locale in TRANSLATED:
        path = FRESH / f"{locale}.json"
        if not path.exists():
            raise SystemExit(f"Fresh has no locale file for {locale}: {path}")
        fresh = load(path)
        table = {}
        for gvim_key, fresh_key in mapping.items():
            text = fresh.get(fresh_key)
            if text is None:
                continue  # Fresh does not translate this key for the locale: fall back to English.
            if placeholders(text) != placeholders(en[gvim_key]):
                raise SystemExit(
                    f"{locale}: {gvim_key} placeholders {placeholders(text)} do not match English "
                    f"{placeholders(en[gvim_key])} ({fresh_key})"
                )
            table[gvim_key] = text
        data = json.dumps(dict(sorted(table.items())), ensure_ascii=False, indent=2) + "\n"
        out = LOCALES / f"{locale}.json"
        current = out.read_text(encoding="utf-8") if out.exists() else None
        if current != data:
            stale += 1
            if check:
                print(f"stale: {out.relative_to(ROOT)}", file=sys.stderr)
            else:
                out.write_text(data, encoding="utf-8")
                print(f"wrote {out.relative_to(ROOT)} ({len(table)} keys)")
        elif not check:
            print(f"{out.relative_to(ROOT)} is up to date ({len(table)} keys)")
    if check:
        if stale:
            raise SystemExit(f"{stale} locale file(s) are stale; run python3 tools/i18n_import.py")
        print("i18n locales are up to date")
    return 0


if __name__ == "__main__":
    sys.exit(main())
