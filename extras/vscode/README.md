# MX Script — VS Code extension

Syntax highlighting and language support for [MX Script](https://github.com/jlkdevelop/mxscript) (`.mx` files).

## Features

- Syntax highlighting for keywords, strings (with `${...}` interpolation), routes, operators, comments
- Bracket auto-closing and surrounding
- Block-aware indentation
- File icon association for `.mx`

## Install

### Via VS Code Marketplace

```
ext install jlkdevelop.mxscript-lang
```

Or search **"MX Script"** in the Extensions panel.

Direct link: <https://marketplace.visualstudio.com/items?itemName=jlkdevelop.mxscript-lang>

### Via prebuilt .vsix

Useful for offline installs or air-gapped environments:

```bash
npx @vscode/vsce package         # builds mxscript-lang-<version>.vsix locally
code --install-extension *.vsix
```

Restart VS Code, open any `.mx` file, and highlighting kicks in.

### From source

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript/extras/vscode
code --install-extension .
```

---

## Publishing to the Marketplace *(maintainers)*

The first publish needs a one-time Azure DevOps setup; after that, every release is one command.

### One-time setup

1. Sign in (or sign up) at [dev.azure.com](https://dev.azure.com) with the GitHub account you want to publish under.
2. Create a Personal Access Token:
   - Click your profile avatar → **Personal access tokens** → **New Token**
   - Organization: **All accessible organizations**
   - Scopes: **Marketplace → Manage**
   - Copy the token (you won't see it again).
3. Create the publisher and authenticate `vsce`:
   ```bash
   npx @vscode/vsce login jlkdevelop
   ```
   Paste the token when prompted. The `jlkdevelop` publisher matches `package.json`'s `publisher` field.

### Each release

From this directory:

```bash
npx @vscode/vsce package      # creates .vsix locally
npx @vscode/vsce publish      # uploads to Marketplace
```

Or in one shot when bumping the version:

```bash
npx @vscode/vsce publish patch   # 0.5.0 → 0.5.1
npx @vscode/vsce publish minor   # 0.5.0 → 0.6.0
npx @vscode/vsce publish major   # 0.5.0 → 1.0.0
```

---

## Contributing

The grammar lives in [`syntaxes/mxscript.tmLanguage.json`](syntaxes/mxscript.tmLanguage.json). It's a standard TextMate grammar — every editor that speaks TextMate (Sublime, Zed, Atom, etc.) can use it directly.

## License

[MIT](./LICENSE) © Jassim Alkharafi
