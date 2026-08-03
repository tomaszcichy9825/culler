# Bundled fonts

culler makes no network calls, so both families ship with the application rather than being
fetched from a CDN at runtime. Only the weights the interface actually draws are bundled.

## JetBrains Mono — the whole application chrome

Every label, value, path, badge, button, status line, table cell and section heading is set in
JetBrains Mono. Four weights are used: 400 body, 500 paths and filenames, 600 headings and
keycaps, 700 badges, buttons, the active mode and the wordmark.

- Source: <https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip>
- Version: 2.304
- Files taken from `fonts/webfonts/` inside that archive
- Licence: **SIL Open Font License 1.1** — `OFL.txt` in the archive, copyright 2020 The JetBrains
  Mono Project Authors. (The design spec calls this Apache 2.0; that was true of JetBrains Mono 1.x
  only. The 2.x series is OFL 1.1.)

| File | Weight | sha256 |
|---|---|---|
| `JetBrainsMono-Regular.woff2` | 400 | `a9cb1cd82332b23a47e3a1239d25d13c86d16c4220695e34b243effa999f45f2` |
| `JetBrainsMono-Medium.woff2` | 500 | `086c48dfbea9ddaff1320f7e09399b8e2924e88ce67453721255db3bdbb5a353` |
| `JetBrainsMono-SemiBold.woff2` | 600 | `918edad542a1da608fd2ba8daebaff9ac802309103fe760eed465b8b4e47faf1` |
| `JetBrainsMono-Bold.woff2` | 700 | `c503cc5ec5f8b2c7666b7ecda1adf44bd45f2e6579b2eba0fc292150416588a2` |

Weight 600 is bundled deliberately: the design uses it on keycaps, settings titles and shoot
names, and the original Google Fonts link omitted it, so those would have been synthesised.

## Public Sans — one headline

Public Sans appears in-app exactly once, on the cold-start headline, at 26px / 600. Only
SemiBold is bundled; no other weight and no italic is drawn anywhere.

- Source: <https://github.com/uswds/public-sans/releases/download/v2.001/public-sans-v2.001.zip>
- Version: 2.001
- File taken from `fonts/webfonts/` inside that archive
- Licence: **SIL Open Font License 1.1** — `LICENSE.md` in the archive. Public Sans is a modified
  version of Libre Franklin by the US General Services Administration; the OFL covers both the
  original and the modification, and the reserved font names are "Public Sans" and "Libre
  Franklin".

| File | Weight | sha256 |
|---|---|---|
| `PublicSans-SemiBold.woff2` | 600 | `f99ffc265cc790e0f058a9f430a465c88996008327abb0f8561cb713add40d73` |

## Redistribution

Both licences permit bundling inside an application, including a commercial one, provided the
licence text travels with the font and the reserved font names are not used for a modified
version. Neither font here is modified — the files are byte-identical to the release archives,
which is what the checksums above are for.

The `@font-face` rules that load these files live in `frontend/public/style.css`.
