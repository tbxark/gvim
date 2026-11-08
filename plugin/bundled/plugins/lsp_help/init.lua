-- LSP install help: a port of Fresh's plugins/lsp_help.ts (660 lines, mostly a table of
-- per-language server install hints; rust/asm/slang keep their own files in Fresh).
--
-- Fresh's three handlers map onto gvim's events:
--   lsp_server_error    -> remember the failure, put a Fresh-style status line on the row
--   lsp_status_clicked  -> open an action popup with the entry's message, copyable install
--                          commands and gvim-native LSP quick actions
--   action_popup_result -> copy the chosen command, or run the chosen native action
--
-- Reduction vs Fresh: gvim's lsp_server_error payload is only {error} (no language,
-- error_type or server_command), so the language is the active buffer's and the server
-- command in the message is the entry's documented default; the not-found status is
-- inferred from the error text. gvim's LSP status click also opens the native server list,
-- so this popup stacks on top of it (dismissing ours reveals it). There is no
-- workspace-symbol quick action in gvim's command table, so the popup offers definition,
-- references, restart, stop, per-buffer toggle and disable. Fresh has no i18n file; the
-- generic strings here are translated and the per-server install hints stay English.
local gvim = require "gvim"

local t = gvim.i18n.t

-- The install hints, reduced to the language servers a gvim user is likely to configure
-- (the languages gvim ships settings for). Copy rows are capped at 10 per popup by
-- MAX_ACTIONS; each entry keeps at most four.
local SERVERS = {
  {
    id = "go", name = "Go", server = "gopls", languages = { "go" },
    message = [["{server}" provides code completion, diagnostics, and navigation for Go files. Copy the command below to install it.]],
    copies = {
      { id = "copy_go", value = "go install golang.org/x/tools/gopls@latest" },
      { id = "copy_brew", value = "brew install gopls" },
    },
  },
  {
    id = "rust", name = "Rust", server = "rust-analyzer", languages = { "rust" },
    message = [["{server}" provides code completion, diagnostics, and navigation for Rust. Install it through rustup (the component ships with the stable toolchain) or your package manager. See https://rust-analyzer.github.io/]],
    copies = {
      { id = "copy_rustup", value = "rustup component add rust-analyzer" },
      { id = "copy_brew", value = "brew install rust-analyzer" },
    },
  },
  {
    id = "python", name = "Python", server = "pyright", languages = { "python" },
    message = [["{server}" provides completion, diagnostics, and navigation for Python files. Requires Node.js. Copy a command below to install it, or visit https://github.com/python-lsp/python-lsp-server for the alternative.]],
    copies = {
      { id = "copy_npm", value = "npm i -g pyright" },
      { id = "copy_pip", value = "pip install python-lsp-server" },
    },
  },
  {
    id = "typescript", name = "TypeScript", server = "typescript-language-server",
    languages = { "typescript", "javascript", "typescriptreact", "javascriptreact" },
    message = [["{server}" provides code completion, diagnostics, and navigation for TypeScript/JavaScript files. Copy a command below to install it, or search online for your platform.]],
    copies = {
      { id = "copy_npm", value = "npm install -g typescript-language-server typescript" },
      { id = "copy_yarn", value = "yarn global add typescript-language-server typescript" },
      { id = "copy_pnpm", value = "pnpm add -g typescript-language-server typescript" },
    },
  },
  {
    id = "clangd", name = "C/C++", server = "clangd", languages = { "c", "cpp", "objective-c", "objective-cpp" },
    message = [["{server}" provides code completion, diagnostics, and navigation for C/C++ files. Copy a command below to install it for your platform.]],
    copies = {
      { id = "copy_apt", value = "sudo apt install clangd" },
      { id = "copy_brew", value = "brew install llvm" },
      { id = "copy_pacman", value = "sudo pacman -S clang" },
    },
  },
  {
    id = "java", name = "Java", server = "jdtls", languages = { "java" },
    message = [["{server}" (Eclipse JDT Language Server) provides completion, diagnostics, and refactoring for Java. It needs JDK 17+ and a project with a build file. See https://github.com/eclipse-jdtls/eclipse.jdt.ls]],
    copies = {
      { id = "copy_brew", value = "brew install jdtls" },
      { id = "copy_sdk", value = "sdk install java 21-tem" },
    },
  },
  {
    id = "json", name = "JSON", server = "vscode-json-language-server", languages = { "json", "jsonc" },
    message = [["{server}" provides completion and validation for JSON files. Copy the command below to install it.]],
    copies = {
      { id = "copy_npm", value = "npm install -g vscode-langservers-extracted" },
    },
  },
  {
    id = "yaml", name = "YAML", server = "yaml-language-server", languages = { "yaml", "yaml.docker-compose", "yaml.gitlab" },
    message = [["{server}" provides code completion, validation, and schema support for YAML files. Requires Node.js. Copy a command below to install it, or visit https://github.com/redhat-developer/yaml-language-server for details.]],
    copies = {
      { id = "copy_npm", value = "npm i -g yaml-language-server" },
      { id = "copy_brew", value = "brew install yaml-language-server" },
    },
  },
  {
    id = "toml", name = "TOML", server = "taplo", languages = { "toml" },
    message = [["{server}" provides code completion, validation, formatting, and schema support for TOML files (Cargo.toml, pyproject.toml, etc.). Copy a command below to install it, or visit https://taplo.tamasfe.dev/cli/installation.html for details.]],
    copies = {
      { id = "copy_cargo", value = "cargo install taplo-cli --locked" },
      { id = "copy_npm", value = "npm i -g @taplo/cli" },
      { id = "copy_brew", value = "brew install taplo" },
    },
  },
  {
    id = "marksman", name = "Markdown", server = "marksman", languages = { "markdown" },
    message = [["{server}" provides completion, diagnostics, and navigation for Markdown files. Copy a command below to install it, or download a release from https://github.com/artempyanykh/marksman]],
    copies = {
      { id = "copy_brew", value = "brew install marksman" },
      { id = "copy_cargo", value = "cargo install --locked marksman" },
    },
  },
  {
    id = "ruby", name = "Ruby", server = "solargraph", languages = { "ruby" },
    message = [["{server}" provides completion, diagnostics, and navigation for Ruby files. Copy the command below to install it, or visit https://solargraph.org for details.]],
    copies = {
      { id = "copy_gem", value = "gem install solargraph" },
    },
  },
  {
    id = "bash", name = "Bash", server = "bash-language-server", languages = { "bash" },
    message = [["{server}" provides code completion, diagnostics, and navigation for shell scripts. Requires Node.js. Copy a command below to install it, or visit https://github.com/bash-lsp/bash-language-server for details. For linting, also consider ShellCheck.]],
    copies = {
      { id = "copy_npm", value = "npm i -g bash-language-server" },
      { id = "copy_yarn", value = "yarn global add bash-language-server" },
      { id = "copy_pnpm", value = "pnpm add -g bash-language-server" },
    },
  },
  {
    id = "lua", name = "Lua", server = "lua-language-server", languages = { "lua" },
    message = [["{server}" provides completion, diagnostics, and navigation for Lua files. Copy a command below to install it, or download a release from https://github.com/LuaLS/lua-language-server]],
    copies = {
      { id = "copy_brew", value = "brew install lua-language-server" },
      { id = "copy_scoop", value = "scoop install lua-language-server" },
    },
  },
  {
    id = "html", name = "HTML", server = "vscode-html-language-server", languages = { "html" },
    message = [["{server}" provides completion, diagnostics, and formatting for HTML files. Copy the command below to install it.]],
    copies = {
      { id = "copy_npm", value = "npm install -g vscode-langservers-extracted" },
    },
  },
  {
    id = "css", name = "CSS", server = "vscode-css-language-server", languages = { "css", "scss", "sass", "less" },
    message = [["{server}" provides code completion, diagnostics, and formatting for CSS files. Copy the command below to install it.]],
    copies = {
      { id = "copy_npm", value = "npm install -g vscode-langservers-extracted" },
    },
  },
  {
    id = "sql", name = "SQL", server = "sqls", languages = { "sql" },
    message = [["{server}" provides completion, hover, and diagnostics for SQL files. It requires a config.yml to connect to your database. See https://github.com/sqls-server/sqls]],
    copies = {
      { id = "copy_go", value = "go install github.com/sqls-server/sqls@latest" },
      { id = "copy_brew", value = "brew install sqls" },
      { id = "copy_npm", value = "npm install -g sql-language-server" },
    },
  },
}

-- One popup takes at most 16 actions: keep room for the seven native rows below.
local MAX_COPIES = 8
local MAX_MESSAGE = 4000

local by_language = {}
for _, help in ipairs(SERVERS) do
  for _, language in ipairs(help.languages) do
    if by_language[language] == nil then
      by_language[language] = help
    end
  end
end

-- failures: language -> { error = text }; pending is the popup whose result is awaited.
local failures = {}
local pending = nil

local function first_line(text)
  text = tostring(text or "")
  local cut = text:find("\n", 1, true)
  if cut then
    text = text:sub(1, cut - 1)
  end
  return gvim.text.trim(text)
end

local function clip(text, limit)
  text = tostring(text or "")
  if #text <= limit then
    return text
  end
  return text:sub(1, limit) .. "..."
end

local function active_language()
  local buf = gvim.buf.current()
  if not buf or buf:is_virtual() then
    return nil
  end
  return buf:language()
end

local function expand(template, help, error_text)
  local text = tostring(template or "")
  text = text:gsub("{server}", help.server or help.name)
  text = text:gsub("{message}", error_text or "")
  return clip(text, MAX_MESSAGE)
end

local function looks_not_found(error_text)
  local lower = (error_text or ""):lower()
  return lower:find("not found", 1, true) ~= nil
    or lower:find("no such file", 1, true) ~= nil
    or lower:find("executable file not found", 1, true) ~= nil
end

gvim.on("lsp_server_error", function(ev)
  local language = active_language()
  local help = language and by_language[language]
  if not help then
    return
  end
  local message = first_line(ev and ev.error)
  failures[language] = { error = message or "" }
  gvim.log.debug("lsp_help: %s: %s", help.id, clip(message, 200))
  if looks_not_found(message) then
    gvim.notify(t("status.server_not_found", { name = help.name }))
  else
    gvim.notify(t("status.server_error", { name = help.name, error = clip(message, 160) }))
  end
end)

local function popup_actions(help)
  local actions = {}
  local copies = help.copies or {}
  for i = 1, math.min(#copies, MAX_COPIES) do
    local copy = copies[i]
    actions[#actions + 1] = {
      id = "copy:" .. copy.id,
      label = clip(copy.label or t("popup.copy", { value = copy.value }), 200),
    }
  end
  local quick = {
    { id = "act:lsp_restart", label = t("actions.restart") },
    { id = "act:lsp_stop", label = t("actions.stop") },
    { id = "act:lsp_toggle_for_buffer", label = t("actions.toggle_buffer") },
    { id = "act:lsp_disable_language", label = t("actions.disable", { name = help.name }) },
    { id = "act:lsp_goto_definition", label = t("actions.definition") },
    { id = "act:lsp_references", label = t("actions.references") },
    { id = "dismiss", label = t("actions.dismiss") },
  }
  for _, action in ipairs(quick) do
    actions[#actions + 1] = action
  end
  return actions
end

local function show_help(language, help)
  local failure = failures[language]
  if not failure then
    return
  end
  if pending ~= nil then
    -- One popup at a time: a second click while ours is up is the same request.
    return
  end
  local actions = popup_actions(help)
  pending = { language = language, help = help }
  local title = clip(help.title or t("popup.title", { name = help.name }), 120)
  local message = expand(help.message, help, failure.error)
  -- The popup is opened without blocking this handler: the choice comes back through
  -- action_popup_result (Fresh's design; the blocking return is deliberately unused).
  gvim.async.spawn(function()
    gvim.ui.action_popup { title = title, message = message, actions = actions }
  end)
end

gvim.on("lsp_status_clicked", function()
  local language = active_language()
  if not language then
    return
  end
  local help = by_language[language]
  if not help or not failures[language] then
    return
  end
  show_help(language, help)
end)

gvim.on("action_popup_result", function(ev)
  if pending == nil then
    return
  end
  local handled = pending
  pending = nil
  local action = ev and ev.action
  if action == nil or action == "dismiss" then
    return
  end
  local help = handled.help
  if action:sub(1, 5) == "copy:" then
    local wanted = action:sub(6)
    for _, copy in ipairs(help.copies or {}) do
      if copy.id == wanted then
        gvim.clipboard.set(copy.value)
        gvim.notify(t("status.copied", { value = copy.value }))
        return
      end
    end
    return
  end
  if action:sub(1, 4) == "act:" then
    local name = action:sub(5)
    gvim.command.run(name)
    if name == "lsp_disable_language" then
      gvim.notify(t("status.disabled", { name = help.name }))
      failures[handled.language] = nil
    end
  end
end)

gvim.log.debug("lsp help plugin loaded (%d language servers)", #SERVERS)
