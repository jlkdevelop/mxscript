# Submitting MX Script to github-linguist

GitHub uses [github-linguist](https://github.com/github-linguist/linguist) to detect, highlight, and count languages. To get **`.mx` officially recognized** with native GitHub syntax highlighting, MX Script needs to be added to linguist's `languages.yml`.

## Inclusion criteria (from linguist's docs)

Linguist accepts a language when it has:

1. **A working TextMate grammar** ✅ — see [`extras/syntax/mxscript.tmLanguage.json`](../syntax/mxscript.tmLanguage.json)
2. **An ace-mode entry** (or grammar repo)
3. **An `interpreters` list** (for shebangs)
4. **Sample `.mx` files** ✅ — see [`samples/`](samples/)
5. **In use across the public ecosystem** — historically ~200+ unique repos. This is the bootstrap problem; we get there by shipping good tooling and getting users.

## What's done

- ✅ TextMate grammar in `extras/syntax/mxscript.tmLanguage.json`
- ✅ VS Code extension scaffold in `extras/vscode/`
- ✅ Sample programs in `examples/`
- ✅ `.gitattributes` set so GitHub knows `.mx` is detectable

## What's left for the linguist PR

When we hit the public-usage threshold, the PR adds an entry like this to linguist's `languages.yml`:

```yaml
MX Script:
  type: programming
  color: "#00d4a3"
  extensions:
    - ".mx"
  tm_scope: source.mx
  ace_mode: text
  interpreters:
    - mx
  language_id: 9000   # assigned by linguist
```

Plus copies of the grammar into linguist's `vendor/grammars/` directory and sample programs into `samples/MX Script/`.

The PR template asks: who maintains the language? Who uses it? Link to docs / spec / homepage.

Answers we'll have ready:

- **Maintainer:** Jassim Alkharafi (founder), [@jlkdevelop](https://github.com/jlkdevelop)
- **Homepage:** https://mxscript.com
- **Repo:** https://github.com/jlkdevelop/mxscript
- **Docs:** https://github.com/jlkdevelop/mxscript/tree/main/docs

## Status

🟡 Awaiting public adoption threshold. Until then, `.mx` files render as plain text on GitHub — but install the [VS Code extension](../vscode/) for full highlighting locally.
