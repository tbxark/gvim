#!/usr/bin/env python3
"""Import Fresh's translations for the gvim i18n catalogue.

The gvim English catalogue (i18n/locales/en.json) is the source of truth. This tool
fills the translated locale files (i18n/locales/<locale>.json) from Fresh's locale
JSON (refs/fresh/crates/fresh-editor-core/locales) and, for a native equivalent of a
Fresh plugin command, from that plugin's own catalogue
(refs/fresh/crates/fresh-editor/plugins/<name>.i18n.json), using the explicit mapping in
i18n/fresh_map.json (gvim key -> Fresh key, or "plugin:<name>:<Fresh key>"). Only keys
whose English wording/meaning matches Fresh are mapped; gvim-only keys stay absent from
the translated files and fall back to English at runtime. refs/fresh is read only by this
generation/check step: the runtime embeds the committed locale files and never reads refs.

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
PLUGINS = ROOT / "refs" / "fresh" / "crates" / "fresh-editor" / "plugins"

# The translated locales: every language gvim offers except English. Fresh also
# ships the other languages, but gvim deliberately embeds only Simplified Chinese.
TRANSLATED = ["zh-CN"]


def load(path):
    with path.open(encoding="utf-8") as f:
        return json.load(f)


_plugin_cache = {}


def plugin_data(name):
    """Load one Fresh plugin's per-locale catalogue (cached)."""
    if name not in _plugin_cache:
        path = PLUGINS / f"{name}.i18n.json"
        if not path.exists():
            raise SystemExit(f"fresh_map.json names a Fresh plugin catalogue that does not exist: {name}")
        _plugin_cache[name] = load(path)
    return _plugin_cache[name]


def source_info(source):
    """Split a fresh_map value into (kind, name, key): core Fresh key or plugin:<name>:<key>."""
    if source.startswith("plugin:"):
        parts = source.split(":", 2)
        if len(parts) != 3 or not parts[1] or not parts[2]:
            raise SystemExit(f"fresh_map.json has a malformed plugin source {source!r}")
        return "plugin", parts[1], parts[2]
    return "core", "", source


def source_english(source):
    """The Fresh English text of a mapping source, validating that the source key exists."""
    kind, name, key = source_info(source)
    if kind == "plugin":
        english = plugin_data(name).get("en", {}).get(key)
        if english is None:
            raise SystemExit(f"fresh_map.json names a Fresh plugin key that does not exist: {source}")
        return english
    return None


def source_text(source, locale, fresh):
    """The translated text of a mapping source for locale, or None when the source lacks it."""
    kind, name, key = source_info(source)
    if kind == "plugin":
        return plugin_data(name).get(locale, {}).get(key)
    return fresh.get(key)


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
    missing_fresh = []
    for gvim_key, source in mapping.items():
        if source.startswith("plugin:"):
            source_english(source)  # validates the plugin catalogue and its English key
        elif source not in fresh_en:
            missing_fresh.append(f"{gvim_key}->{source}")
    if missing_fresh:
        raise SystemExit("fresh_map.json names Fresh keys that do not exist: " + ", ".join(missing_fresh))

    stale = 0
    for locale in TRANSLATED:
        path = FRESH / f"{locale}.json"
        if not path.exists():
            raise SystemExit(f"Fresh has no locale file for {locale}: {path}")
        fresh = load(path)
        table = {}
        # Explicit mappings first: gvim keys whose Fresh key differs (or that are
        # gvim-only in English but reuse a Fresh translation).
        for gvim_key, source in mapping.items():
            text = source_text(source, locale, fresh)
            if text is None:
                continue  # Fresh does not translate this key for the locale: fall back to English.
            if placeholders(text) != placeholders(en[gvim_key]):
                raise SystemExit(
                    f"{locale}: {gvim_key} placeholders {placeholders(text)} do not match English "
                    f"{placeholders(en[gvim_key])} ({source})"
                )
            table[gvim_key] = text
        # Identity imports: catalogue keys that ARE Fresh's key (generated command and
        # menu keys), imported when Fresh's English text for that key is exactly ours.
        # This keeps fresh_map.json to the keys whose Fresh key differs.
        for key, english in en.items():
            if key in mapping or key.startswith("gvim."):
                continue
            if fresh_en.get(key) != english:
                continue
            text = fresh.get(key)
            if text is None:
                continue
            if placeholders(text) != placeholders(english):
                raise SystemExit(
                    f"{locale}: {key} placeholders {placeholders(text)} do not match English "
                    f"{placeholders(english)}"
                )
            table[key] = text
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
