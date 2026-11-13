-- The install hints of lsp_help, read on the first language server error so that they cost
-- no heap while every server works. Reduced to the language servers a gvim user is likely to
-- configure (the languages gvim ships settings for); the popup shows at most eight copy rows
-- of an entry.
return {
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
