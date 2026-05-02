# MX Script — VS Code extension

Syntax highlighting and basic language support for [MX Script](https://github.com/jlkdevelop/mxscript) (`.mx` files).

## Features

- Syntax highlighting for keywords, strings (with `${...}` interpolation), routes, operators, comments
- Bracket auto-closing and surrounding
- Block-aware indentation
- File icon association for `.mx`

## Install (local development)

While we wait for the Marketplace listing, you can install the extension straight from this folder:

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript/extras/vscode
code --install-extension .
```

Or symlink it into your VS Code extensions directory:

```bash
ln -s "$(pwd)" "$HOME/.vscode/extensions/mxscript-0.5.0"
```

Restart VS Code, open any `.mx` file, and highlighting should kick in.

## Install (Marketplace)

Coming soon. Track progress at [#vscode-marketplace](https://github.com/jlkdevelop/mxscript/issues).

## Contributing

The grammar lives in [`syntaxes/mxscript.tmLanguage.json`](syntaxes/mxscript.tmLanguage.json). It's a standard TextMate grammar — every editor that speaks TextMate (Sublime, Atom, TextMate, etc.) can use it directly.

## License

MIT © Jassim Alkharafi
