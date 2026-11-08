package manager

import (
	"slices"
	"sort"
	"strings"
)

// Spec is one built-in language server entry: the program that serves a language, how
// to get it, and how gvim treats it. The table mirrors the default `lsp` map of Fresh
// (crates/fresh-editor-core/src/config.rs and docs/features/lsp.md).
type Spec struct {
	// Language is the key of Fresh's table, the name its config files use.
	Language string
	// Command and Args launch the server over stdio.
	Command string
	Args    []string
	// Install says how to get the server. It is shown when the command is not on PATH.
	Install string
	// Manual marks a server that is not started on its own, even when it is installed:
	// Fresh ships `ols` and the Godot server that way. The user turns it on with an `lsp`
	// entry for the language.
	Manual bool
	// Aliases are the other names gvim's language detection gives the language, in lower
	// case, such as "c++" for cpp. They are served by the same entry.
	Aliases []string
	// LanguageID is the languageId that didOpen carries when it differs from Language.
	LanguageID string
	// RootMarkers are the names (or globs) of the files that mark the root of a project, as in
	// Fresh's table. The server of a file runs in the nearest directory above the file that
	// holds one of them. A language with none runs in the workspace root.
	RootMarkers []string
}

// Defaults returns the built-in server table in order of language.
func Defaults() []Spec {
	out := slices.Clone(defaultSpecs)
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out
}

// Names returns the names a language is known by in a server map: its own name and its
// aliases.
func (s Spec) Names() []string {
	return append([]string{s.Language}, s.Aliases...)
}

// LanguageKey returns the key of the built-in table for a language name as gvim's detection
// gives it (lower case), or the name itself when the table does not know it.
func LanguageKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, s := range defaultSpecs {
		if s.Language == name || slices.Contains(s.Aliases, name) {
			return s.Language
		}
	}
	return name
}

// DefaultFor returns the built-in entry that serves a language name, or false.
func DefaultFor(name string) (Spec, bool) {
	key := LanguageKey(name)
	for _, s := range defaultSpecs {
		if s.Language == key {
			return s, true
		}
	}
	return Spec{}, false
}

// The npm, brew and other commands below are the ones the servers' own documentation gives.
// Where Fresh's docs name a command, it is that one.
var defaultSpecs = []Spec{
	{Language: "rust", Command: "rust-analyzer", Install: "rustup component add rust-analyzer", RootMarkers: []string{"Cargo.toml", "rust-project.json", ".git"}},
	{Language: "python", Command: "pylsp", Install: "pip install python-lsp-server", RootMarkers: []string{"pyproject.toml", "setup.py", "setup.cfg", "pyrightconfig.json", ".git"}},
	{Language: "go", Command: "gopls", Install: "go install golang.org/x/tools/gopls@latest", RootMarkers: []string{"go.mod", "go.work", ".git"}},
	{Language: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"},
		Install: "npm install -g typescript-language-server typescript", RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"}},
	{Language: "tsx", Command: "typescript-language-server", Args: []string{"--stdio"},
		Install: "npm install -g typescript-language-server typescript", LanguageID: "typescriptreact", RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"}},
	{Language: "javascript", Command: "typescript-language-server", Args: []string{"--stdio"},
		Install: "npm install -g typescript-language-server typescript", RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"}},
	{Language: "jsx", Command: "typescript-language-server", Args: []string{"--stdio"},
		Install: "npm install -g typescript-language-server typescript", Aliases: []string{"react"}, LanguageID: "javascriptreact", RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"}},
	{Language: "html", Command: "vscode-html-language-server", Args: []string{"--stdio"}, Install: "npm install -g vscode-langservers-extracted"},
	{Language: "css", Command: "vscode-css-language-server", Args: []string{"--stdio"}, Install: "npm install -g vscode-langservers-extracted"},
	{Language: "json", Command: "vscode-json-language-server", Args: []string{"--stdio"}, Install: "npm install -g vscode-langservers-extracted"},
	{Language: "jsonc", Command: "vscode-json-language-server", Args: []string{"--stdio"}, Install: "npm install -g vscode-langservers-extracted"},
	{Language: "c", Command: "clangd", Install: "brew install llvm", RootMarkers: []string{"compile_commands.json", "CMakeLists.txt", "Makefile", ".git"}},
	{Language: "cpp", Command: "clangd", Install: "brew install llvm", Aliases: []string{"c++"}, LanguageID: "cpp", RootMarkers: []string{"compile_commands.json", "CMakeLists.txt", "Makefile", ".git"}},
	{Language: "csharp", Command: "csharp-ls", Install: "dotnet tool install -g csharp-ls", Aliases: []string{"c#"}, RootMarkers: []string{"*.csproj", "*.sln", ".git"}},
	{Language: "zig", Command: "zls", Install: "brew install zls"},
	{Language: "odin", Command: "ols", Manual: true, Install: "build from source: https://github.com/DanielGavin/ols", RootMarkers: []string{"ols.json", ".git"}},
	{Language: "c3", Command: "c3lsp", Install: "see https://github.com/pherrymason/c3-lsp", RootMarkers: []string{"project.json", ".git"}},
	{Language: "slang", Command: "slangd", Install: "slangd comes with the Slang compiler: https://shader-slang.org"},
	{Language: "java", Command: "jdtls", Install: "brew install jdtls", RootMarkers: []string{"pom.xml", "build.gradle", "build.gradle.kts", ".git"}},
	{Language: "latex", Command: "texlab", Install: "brew install texlab", Aliases: []string{"tex"}},
	{Language: "markdown", Command: "marksman", Args: []string{"server"}, Install: "brew install marksman"},
	{Language: "templ", Command: "templ", Args: []string{"lsp"}, Install: "go install github.com/a-h/templ/cmd/templ@latest"},
	{Language: "typst", Command: "tinymist", Install: "brew install tinymist"},
	{Language: "bash", Command: "bash-language-server", Args: []string{"start"},
		Install: "npm install -g bash-language-server", Aliases: []string{"shell", "sh", "shellscript", "zsh"}},
	{Language: "lua", Command: "lua-language-server", Install: "brew install lua-language-server", RootMarkers: []string{".luarc.json", ".luarc.jsonc", ".luacheckrc", ".stylua.toml", ".git"}},
	{Language: "ruby", Command: "solargraph", Args: []string{"stdio"}, Install: "gem install solargraph", RootMarkers: []string{"Gemfile", ".ruby-version", ".git"}},
	{Language: "php", Command: "phpactor", Args: []string{"language-server"}, Install: "brew install phpactor", RootMarkers: []string{"composer.json", ".git"}},
	{Language: "yaml", Command: "yaml-language-server", Args: []string{"--stdio"}, Install: "npm install -g yaml-language-server"},
	{Language: "toml", Command: "taplo", Args: []string{"lsp", "stdio"}, Install: "brew install taplo"},
	{Language: "dart", Command: "dart", Args: []string{"language-server", "--protocol=lsp"}, Install: "install the Dart SDK: https://dart.dev/get-dart", RootMarkers: []string{"pubspec.yaml", ".git"}},
	{Language: "nushell", Command: "nu", Args: []string{"--lsp"}, Install: "brew install nushell", Aliases: []string{"nu"}},
	{Language: "solidity", Command: "nomicfoundation-solidity-language-server", Args: []string{"--stdio"},
		Install: "npm install -g @nomicfoundation/solidity-language-server"},
	{Language: "terraform", Command: "terraform-ls", Args: []string{"serve"},
		Install: "brew install hashicorp/tap/terraform-ls", Aliases: []string{"hcl"}, RootMarkers: []string{"*.tf", ".terraform", ".git"}},
	{Language: "cmake", Command: "cmake-language-server", Install: "pip install cmake-language-server", RootMarkers: []string{"CMakeLists.txt", ".git"}},
	{Language: "protobuf", Command: "buf", Args: []string{"beta", "lsp"},
		Install: "brew install bufbuild/buf/buf", Aliases: []string{"protocol buffer", "protocol buffers"}},
	{Language: "graphql", Command: "graphql-lsp", Args: []string{"server", "-m", "stream"}, Install: "npm install -g graphql-language-service-cli"},
	{Language: "sql", Command: "sqls", Install: "go install github.com/sqls-server/sqls@latest", Aliases: []string{"mysql"}},
	{Language: "vue", Command: "vue-language-server", Args: []string{"--stdio"}, Install: "npm install -g @vue/language-server"},
	{Language: "svelte", Command: "svelteserver", Args: []string{"--stdio"}, Install: "npm install -g svelte-language-server"},
	{Language: "astro", Command: "astro-ls", Args: []string{"--stdio"}, Install: "npm install -g @astrojs/language-server"},
	{Language: "tailwindcss", Command: "tailwindcss-language-server", Args: []string{"--stdio"}, Install: "npm install -g @tailwindcss/language-server"},
	{Language: "nix", Command: "nil", Install: "nix profile install nixpkgs#nil"},
	{Language: "kotlin", Command: "kotlin-lsp", Args: []string{"--stdio"},
		Install: "brew install JetBrains/utils/kotlin-lsp, or the standalone archive: https://github.com/Kotlin/kotlin-lsp/releases", RootMarkers: []string{"settings.gradle.kts", "settings.gradle", "pom.xml", "workspace.json"}},
	{Language: "swift", Command: "sourcekit-lsp", Install: "sourcekit-lsp comes with the Swift toolchain: https://swift.org/install"},
	{Language: "scala", Command: "metals", Install: "cs install metals (coursier: https://get-coursier.io)"},
	{Language: "elixir", Command: "elixir-ls", Install: "brew install elixir-ls"},
	{Language: "erlang", Command: "erlang_ls", Install: "brew install erlang_ls"},
	{Language: "haskell", Command: "haskell-language-server-wrapper", Args: []string{"--lsp"}, Install: "ghcup install hls"},
	{Language: "ocaml", Command: "ocamllsp", Install: "opam install ocaml-lsp-server"},
	{Language: "clojure", Command: "clojure-lsp", Install: "brew install clojure-lsp/brew/clojure-lsp-native"},
	{Language: "r", Command: "R", Args: []string{"--vanilla", "-e", "languageserver::run()"}, Install: `install.packages("languageserver") in R`},
	{Language: "julia", Command: "julia",
		Args:    []string{"--startup-file=no", "--history-file=no", "-e", "using LanguageServer; runserver()"},
		Install: `using Pkg; Pkg.add("LanguageServer") in Julia`},
	{Language: "perl", Command: "perlnavigator", Args: []string{"--stdio"}, Install: "npm install -g perlnavigator-server"},
	{Language: "nim", Command: "nimlangserver", Install: "nimble install nimlangserver"},
	{Language: "gleam", Command: "gleam", Args: []string{"lsp"}, Install: "gleam comes with its own server: https://gleam.run/getting-started/installing/"},
	{Language: "racket", Command: "racket-langserver", Install: "raco pkg install racket-langserver", RootMarkers: []string{"info.rkt", ".git"}},
	{Language: "fsharp", Command: "fsautocomplete", Args: []string{"--adaptive-lsp-server-enabled"},
		Install: "dotnet tool install -g fsautocomplete", Aliases: []string{"f#"}},
	// svls serves both Verilog and SystemVerilog, asm-lsp the assembly languages.
	{Language: "verilog", Command: "svls", Install: "cargo install svls", RootMarkers: []string{".svls.toml", ".git"}},
	{Language: "systemverilog", Command: "svls", Install: "cargo install svls", RootMarkers: []string{".svls.toml", ".git"}},
	{Language: "asm", Command: "asm-lsp", Install: "cargo install asm-lsp", Aliases: []string{"nasm"}, RootMarkers: []string{".asm-lsp.toml", ".git"}},
	{Language: "gas", Command: "asm-lsp", Install: "cargo install asm-lsp", RootMarkers: []string{".asm-lsp.toml", ".git"}},
	// Godot's server is a TCP port of a running editor, reached through netcat. Fresh turns it
	// off by default, because most machines have no Godot running, and so does gvim.
	{Language: "gdscript", Command: "nc", Args: []string{"127.0.0.1", "6005"}, Manual: true, Aliases: []string{"gdscript3"},
		Install: "start the Godot editor, which listens on 127.0.0.1:6005", RootMarkers: []string{"project.godot", ".git"}},
}
