#!/usr/bin/env python3
"""Generate internal/tui/menubar/menu_table_gen.go from the legacy Fresh menu source.

The menu bar is read from refs/fresh/crates/fresh-editor-core/src/config.rs,
function MenuConfig::translated_menus(). Labels come from locales/en.json, the
context-key names from crates/fresh-editor-core/src/types.rs, shortcuts from the
macOS keymap merged over the default keymap, and the version label from the root
Cargo.toml. The output is committed. This script is run by hand and never at
build time.

Shortcut rule, from refs/fresh/crates/fresh-editor/src/input/keybindings.rs
find_keybinding_for_action: among the bindings of the action in the normal
context (then global), prefer the fewest modifiers, then the lowest modifier
bits (shift=1, ctrl=2, alt=4), then the key name.

Usage: python3 tools/gen_menus.py [--check]
  --check   compare the generated text with the committed file and exit 1 on drift.
"""

import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LEGACY = ROOT / "refs" / "fresh"
CONFIG_RS = LEGACY / "crates" / "fresh-editor-core" / "src" / "config.rs"
TYPES_RS = LEGACY / "crates" / "fresh-editor-core" / "src" / "types.rs"
EN_JSON = LEGACY / "crates" / "fresh-editor-core" / "locales" / "en.json"
DEFAULT_KEYMAP = LEGACY / "crates" / "fresh-editor-core" / "keymaps" / "default.json"
MACOS_KEYMAP = LEGACY / "crates" / "fresh-editor-core" / "keymaps" / "macos.json"
CARGO_TOML = LEGACY / "Cargo.toml"
OUT = ROOT / "internal" / "tui" / "menubar" / "menu_table_gen.go"

FN_SIGNATURE = "pub fn translated_menus() -> Vec<Menu> {"
ITEM_RE = re.compile(r"MenuItem::(Action|Separator|Submenu|DynamicSubmenu|Label)\s*\{")
MENU_RE = re.compile(r"(?<![A-Za-z])Menu \{")
PAIRS = {"{": "}", "(": ")", "[": "]"}
MOD_BITS = {"shift": 1, "ctrl": 2, "alt": 4}
MOD_SYMBOL = {"ctrl": "⌃", "alt": "⌥", "shift": "⇧"}
KEY_SYMBOL = {"Escape": "Esc", "Enter": "⏎", "Backspace": "⌫", "Delete": "⌦", " ": "Space", "Tab": "⇥", "BackTab": "⇤"}


def skip_string(s, j):
    """Return the index just past the Rust string literal that starts at s[j]."""
    j += 1
    while s[j] != '"':
        j += 2 if s[j] == "\\" else 1
    return j + 1


def match_close(s, i):
    """Return the index of the bracket that closes the one at s[i].

    String literals and line comments are skipped, so braces inside them do
    not count.
    """
    stack = []
    j = i
    while j < len(s):
        c = s[j]
        if c == '"':
            j = skip_string(s, j)
            continue
        if s.startswith("//", j):
            j = s.find("\n", j)
            if j < 0:
                break
            continue
        if c in PAIRS:
            stack.append(PAIRS[c])
        elif c in "})]":
            if not stack or stack.pop() != c:
                raise ValueError(f"unbalanced {c!r} at offset {j}")
            if not stack:
                return j
        j += 1
    raise ValueError(f"no closing bracket for {s[i]!r} at offset {i}")


def load_context_keys():
    text = TYPES_RS.read_text(encoding="utf-8")
    keys = dict(re.findall(r'pub const ([A-Z0-9_]+): &str = "([^"]+)";', text))
    if not keys:
        raise SystemExit("no context keys found in types.rs")
    return keys


def load_keymap(path):
    """Return {(key, mods, when): action} for a keymap file, applying its unbinds."""
    data = json.loads(path.read_text(encoding="utf-8"))
    return data.get("bindings", []), data.get("inherits")


def merged_bindings():
    default, _ = load_keymap(DEFAULT_KEYMAP)
    macos, inherits = load_keymap(MACOS_KEYMAP)
    if inherits != "default":
        raise SystemExit(f"macos keymap inherits {inherits!r}, expected 'default'")
    merged = {}
    for b in default + macos:
        k = (b["key"], tuple(sorted(b.get("modifiers", []))), b.get("when", "normal"))
        if b["action"] == "unbind":
            merged.pop(k, None)
        else:
            merged[k] = b["action"]
    return merged


def shortcut(action, merged):
    for when in ("normal", "global"):
        cands = [(mods, key) for (key, mods, w), act in merged.items() if act == action and w == when]
        if cands:
            mods, key = min(cands, key=lambda c: (len(c[0]), sum(MOD_BITS[m] for m in c[0]), c[1]))
            head = "".join(MOD_SYMBOL[m] for m in ("ctrl", "alt", "shift") if m in mods)
            name = KEY_SYMBOL.get(key, key.upper() if len(key) == 1 else key)
            return head + name
    return ""


def match_brace_at(s, marker_index):
    return match_close(s, s.index("{", marker_index))


def ctx_of(body, field, ctx_keys):
    m = re.search(r"\b" + field + r":\s*(None|Some\(context_keys::([A-Z0-9_]+)\.to_string\(\)\))", body)
    if not m or m.group(1) == "None":
        return ""
    name = m.group(2)
    if name not in ctx_keys:
        raise SystemExit(f"unknown context key {name}")
    return ctx_keys[name]


# The product shown to users is gvim. The legacy labels name Fresh, the program
# this port is taken from, so each one is rewritten here. Exact labels come first;
# the rest of the text has "Fresh" replaced by PRODUCT.
PRODUCT = "gvim"
PRODUCT_LABELS = {
    # gvim has no manual; F1 is the help action.
    "Show Fresh Manual": "Show Help",
}

# Labels gvim rewrites get a gvim-only catalogue key: Fresh's translation would name
# Fresh, so the translated row must not use the Fresh key.
PRODUCT_KEYS = {
    "Show Fresh Manual": "gvim.menu.show_help",
}


def product_label(label):
    if label in PRODUCT_LABELS:
        return PRODUCT_LABELS[label]
    return label.replace("Fresh", PRODUCT)


def label_of(body, en):
    """Return (display label, catalogue key, original English label).

    The catalogue key is Fresh's i18n key for the label (menu.<menu>.<item>), empty
    for a label with no key; a label gvim rewrites gets its gvim-only key instead.
    """
    m = re.search(r'\blabel:\s*t!\("([^"]+)"\)', body)
    if m:
        key = m.group(1)
        if key not in en:
            raise SystemExit(f"missing locale key {key}")
        original = en[key]
        if original in PRODUCT_KEYS:
            return product_label(original), PRODUCT_KEYS[original], original
        return product_label(original), key, original
    m = re.search(r'\blabel:\s*"((?:[^"\\]|\\.)*)"', body)
    if not m:
        raise SystemExit("item without a label: " + body[:80])
    original = m.group(1)
    return product_label(original), "", original


def parse_items(text, en, ctx_keys, version, stats):
    items = []
    i = 0
    while True:
        m = ITEM_RE.search(text, i)
        if not m:
            break
        ob = m.end() - 1
        cb = match_close(text, ob)
        body = text[ob + 1 : cb]
        i = cb + 1
        kind = m.group(1)
        if kind == "Action":
            action = re.search(r'\baction:\s*"([^"]+)"', body).group(1)
            when = ctx_of(body, "when", ctx_keys)
            checkbox = ctx_of(body, "checkbox", ctx_keys)
            label, key, _ = label_of(body, en)
            items.append({"kind": "action", "label": label, "key": key, "action": action,
                          "when": when, "checkbox": checkbox, "children": []})
            stats["leaves"] += 1
            if when:
                stats["leaves_when"] += 1
            if checkbox:
                stats["checkboxes"] += 1
        elif kind == "Separator":
            items.append({"kind": "separator", "children": []})
            stats["separators"] += 1
        elif kind == "Submenu":
            vec = body.index("vec![") + len("vec![") - 1
            vec_end = match_close(body, vec)
            children = parse_items(body[vec + 1 : vec_end], en, ctx_keys, version, stats)
            slabel, skey, _ = label_of(body, en)
            items.append({"kind": "submenu", "label": slabel, "key": skey, "children": children})
            stats["submenus"] += 1
        elif kind == "DynamicSubmenu":
            label, key, _ = label_of(body, en)
            items.append({"kind": "submenu", "label": label, "key": key, "children": []})
            stats["submenus"] += 1
            stats["dynamic_submenus"] += 1
        elif kind == "Label":
            prefix = re.search(r'format!\("([^"{]*)\{\}"', body).group(1)
            # The header names the product only: the version is Fresh's, not gvim's.
            items.append({"kind": "label", "label": PRODUCT, "children": []})
            stats["labels"] += 1
    return items


def parse_menus(src, en, ctx_keys, version, stats):
    start = src.index(FN_SIGNATURE)
    fn_open = src.index("{", start + len(FN_SIGNATURE) - 1)
    fn_close = match_close(src, fn_open)
    region = src[fn_open : fn_close + 1]
    menus = []
    pos = 0
    while True:
        m = MENU_RE.search(region, pos)
        if not m:
            break
        ob = m.end() - 1
        cb = match_close(region, ob)
        body = region[ob + 1 : cb]
        pos = cb + 1
        vec = body.index("items: vec![") + len("items: vec![") - 1
        head = body[:vec]
        ident = re.search(r'id:\s*Some\("([^"]+)"', head).group(1)
        title, title_key, _ = label_of(head, en)
        when = ctx_of(head, "when", ctx_keys)
        vec_end = match_close(body, vec)
        items = parse_items(body[vec + 1 : vec_end], en, ctx_keys, version, stats)
        menus.append({"id": ident, "label": title, "key": title_key, "when": when, "items": items})
    return menus


def go_str(s):
    return json.dumps(s, ensure_ascii=False)


def emit_item(it, merged, indent):
    pad = "\t" * indent
    if it["kind"] == "separator":
        return [f"{pad}{{kind: itemSeparator}},"]
    if it["kind"] == "label":
        return [f"{pad}{{kind: itemLabel, label: {go_str(it['label'])}}},"]
    if it["kind"] == "submenu":
        subkey = f"key: {go_str(it['key'])}, " if it.get("key") else ""
        lines = [f"{pad}{{kind: itemSubmenu, label: {go_str(it['label'])}, {subkey}children: []legacyItem{{"]
        for c in it["children"]:
            lines += emit_item(c, merged, indent + 1)
        lines.append(f"{pad}}}}},")
        return lines
    fields = [f"kind: itemAction", f"label: {go_str(it['label'])}"]
    if it.get("key"):
        fields.append(f"key: {go_str(it['key'])}")
    keys = shortcut(it["action"], merged)
    if keys:
        fields.append(f"keys: {go_str(keys)}")
    fields.append(f"action: {go_str(it['action'])}")
    if it["when"]:
        fields.append(f"when: {go_str(it['when'])}")
    if it["checkbox"]:
        fields.append(f"checkbox: {go_str(it['checkbox'])}")
    return [f"{pad}{{{', '.join(fields)}}},"]


def emit(menus, merged):
    out = [
        "// Code generated by tools/gen_menus.py from the Fresh menu source",
        "// (crates/fresh-editor-core/src/config.rs translated_menus). DO NOT EDIT.",
        "",
        "package menubar",
        "",
        "// legacyMenus is the menu bar of the Fresh TUI, in bar order, as the legacy",
        "// source defines it. keys is the macOS shortcut shown at the right edge. when",
        "// and checkbox name Fresh context keys; menuEnabled and menuChecked evaluate them.",
        "var legacyMenus = []legacyMenu{",
    ]
    for menu in menus:
        out.append("\t{")
        out.append(f"\t\tid: {go_str(menu['id'])},")
        out.append(f"\t\tlabel: {go_str(menu['label'])},")
        if menu["key"]:
            out.append(f"\t\tkey: {go_str(menu['key'])},")
        if menu["when"]:
            out.append(f"\t\twhen: {go_str(menu['when'])},")
        out.append("\t\titems: []legacyItem{")
        for it in menu["items"]:
            out += emit_item(it, merged, 3)
        out.append("\t\t},")
        out.append("\t},")
    out += ["}", ""]
    return "\n".join(out)


def main():
    src = CONFIG_RS.read_text(encoding="utf-8")
    en = json.loads(EN_JSON.read_text(encoding="utf-8"))
    ctx_keys = load_context_keys()
    cargo = CARGO_TOML.read_text(encoding="utf-8")
    version = re.search(r'^version\s*=\s*"([^"]+)"', cargo, re.M).group(1)
    stats = {"leaves": 0, "leaves_when": 0, "checkboxes": 0, "separators": 0,
             "submenus": 0, "dynamic_submenus": 0, "labels": 0}
    menus = parse_menus(src, en, ctx_keys, version, stats)
    text = emit(menus, merged_bindings())
    summary = (f"menus={len(menus)} leaves={stats['leaves']} leaves_with_when={stats['leaves_when']} "
               f"checkboxes={stats['checkboxes']} separators={stats['separators']} "
               f"submenus={stats['submenus']} dynamic_submenus={stats['dynamic_submenus']} labels={stats['labels']}")
    if "--check" in sys.argv[1:]:
        text = subprocess.run(["gofmt"], input=text, capture_output=True, text=True, check=True).stdout
        if OUT.read_text(encoding="utf-8") != text:
            print("drift: " + OUT.relative_to(ROOT).as_posix() + " is stale", file=sys.stderr)
            sys.exit(1)
        print("up to date: " + summary)
        return
    OUT.write_text(text, encoding="utf-8")
    subprocess.run(["gofmt", "-w", str(OUT)], check=True)
    print("wrote " + OUT.relative_to(ROOT).as_posix() + " " + summary)


if __name__ == "__main__":
    main()