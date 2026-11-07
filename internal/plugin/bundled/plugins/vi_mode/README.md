# vi_mode

Vi-style modal editing for gvim, a port of Fresh's `vi_mode` plugin. It ships with gvim and
is off by default. Turn it on in the configuration:

```jsonc
{ "plugins": { "vi_mode": { "enabled": true } } }
```

It starts in normal mode. `Vi Mode: Toggle` (command palette) switches it off and on.
**Escape hatches:** `ctrl+^`, and three `Esc` presses within 600 ms, clear every plugin input
mode, vi's included, whatever state it is in. vi then says so; run `Vi Mode: Toggle` to bring
it back.

## What works

| Area | Keys |
|---|---|
| Modes | normal, insert (`i a I A o O gi gI`), visual (`v V ctrl+v`), `:` command line |
| Counts | `3j`, `d2w`, `2d3w`, `3.`, `5i-<Esc>` |
| Motions | `h j k l`, arrows, space, backspace, `w b e W B E ge gE`, `0 ^ $ g_ \|`, `gg G` (`3G`), `H M L`, `+ - _ enter`, `{ }`, `%`, `f F t T ; ,`, `/ ? n N * # g* g#`, `' \``, `gj gk` |
| Operators | `d c y > <`, `g~ gu gU g?`, doubled (`dd cc yy >> <<`), with any motion or text object |
| Text objects | `iw aw iW aW ip ap i( a( ib` `i[ a[ i{ a{ iB i< a< i" a" i' a' i\` a\` it at` |
| Shortcuts | `x X D C s S Y p P gp gP J gJ ~ r{c} ctrl+a ctrl+x u ctrl+r . &` |
| Registers | `"a`..`"z`, `"A` appends, `"0`..`"9`, `"-`, `"_`, `"+ "*` (clipboard; `"+p` runs the editor's paste) |
| Marks | `ma`, `'a`, `` `a ``, `''`, `'<`, `'>` |
| Visual | all motions and text objects, `d x X D y Y c s C S R > < ~ u U J gJ r p P o : gv I A`; block mode paints with highlights |
| `:` commands | `:N :$ :w :q :wq :x :e :enew :saveas :r :n :N :bn :bp :b :ls :bd :sp :vs :new :only :noh :s :& :g :v :d :y :j :> :< :m :t :sort :set :undo :redo :mark :marks :reg :f :pwd` |
| Scrolling | `ctrl+d ctrl+u ctrl+f ctrl+b ctrl+e ctrl+y zz zt zb` |

`:q` closes the tab (and quits on the last one) and refuses with unsaved changes; `:q!` closes
the tab and drops them; `:qa!` asks before it discards every unsaved buffer and quits.
`:s` and `/` take vi patterns (`\< \> \( \) \| \+ \? \{`); they are turned into Go regular
expressions.

## Settings (`plugins.vi_mode.settings`)

| Key | Default | |
|---|---|---|
| `auto_start` | `true` | start in normal mode when the plugin loads |
| `arrow_keys` | `true` | arrows, Home, End and Page keys act as `h j k l 0 $` |
| `ctrl_keys` | `true` | vi owns `ctrl+r f b d u e y v a x g` in normal and visual mode |
| `ignore_case`, `smart_case` | `true` | search case |
| `shift_width` | `0` | width of `>` and `<` (0: the tab size) |
| `clipboard_yank` | `true` | a yank also sets the system clipboard |
| `search_word_under_cursor` | `true` | `*` and `#` |

## Differences from Vim

* Keys the plugin does not bind, such as `ctrl+s`, `ctrl+q`, `ctrl+p`, `alt+...` and the
  function keys, still reach the editor in every mode; printable keys that vi has no command
  for do nothing in normal and visual mode.
* No macros (`q`, `@`: use the editor's own), no jump list (`ctrl+o`, `ctrl+i`), no replace
  mode (`R`), no sentences (`( ) is as`), no folds, no `:!` filters, no `:normal`.
* Marks do not move when the text around them changes.
* An insert that is repeated with a count (`3ix<Esc>`) or by `.` is one more undo step. The
  editor groups typing by time, so a long insert can take more than one `u` as well.
* A change operator and the text typed after it (`cw foo<Esc>`, `cc`, `C`, `s`) are two undo
  steps: the first `u` takes the typed text away and the second brings the deleted text
  back. A block `I`, `A` or `c` copies the text to the other lines in a later step, so one
  `u` undoes only that copy. The editor has no undo group that stays open while keys are typed.
* After `u`, `guu`, `gUU`, `g~~`, `Vy` and `yip` the caret column can differ from Vim's (Vim
  goes to column 0 or to the start of the range).
* `j` and `k` are the editor's own moves: they go by display rows on a wrapped line, and a
  count counts display rows, but `d3j`, `y3j` and the visual mode count logical lines, and
  the visual mode keeps the wanted column itself. `gj` and `gk` stop at the first and last
  line.
* `.` repeats an insert as the text that ended up between where it began and where it ended;
  an insert that moved the caret with an arrow key is not remembered.
* Only the primary caret is used; extra carets are ignored.
* A file that is still being indexed (large file mode) refuses commands with a message until
  the index is ready.
* Search (`/`, `?`, `n`, `N`, `*`, `#`, `:g`) does not work on a file in large file mode
  (10 MiB or more): it says "no search in large-file mode". Use the editor's own find.
