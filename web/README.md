# havenry-web

Marketing site for [Havenry](https://github.com/aronk11/havenry). Static React,
no backend, no analytics.

```bash
npm install
npm run dev      # http://localhost:5173
npm run build    # → dist/
npm run preview  # serve the build locally
```

## Layout

```
src/
  kit/            the component kit — eight pieces, reusable anywhere
  sections/       page sections, each composed from the kit
  styles/         tokens.css (the design system) + sections.css (rhythm)
  config.js       repo URL and licence, in one place
```

## The one rule

**Thick outline, hard offset shadow, no gradients on surfaces.**

The gradient belongs to `<Sky>` and nowhere else. Blur and glass effects break
the sticker look instantly — that is the edge of this system, not a matter of
taste. If a new component needs one of those, it needs a different system.

## The kit

| Component | What it is for |
|---|---|
| `<Cloud>` | The base shape. One SVG path so the outline stays continuous. |
| `<Sky>` | Page backdrop. Owns the only gradient and the ambient drift. |
| `<HostCloud>` | A machine drawn as a cloud; the squares inside are containers. |
| `<PuffButton>` | The only control. Sinks by exactly its shadow offset. |
| `<Tag>` | Status pill. Colour never carries meaning on its own. |
| `<Panel>` | Container with corners, for text that needs edges. |
| `<CodeLine>` | One command with a copy button that resets itself. |
| `<StepRail>` | A numbered sequence — only where order truly matters. |

Nothing in `kit/` knows which section it sits in. That is what makes the pieces
reusable rather than merely extracted.

## Deployment

`npm run build` produces a static `dist/`. Any host works — Pages, Netlify,
Caddy, or a Havenry-managed container.

## Forking

If you run a modified Havenry, AGPL §13 requires the running instance to point
at *your* source. Change `REPO_URL` in `src/config.js` and the corresponding
`SourceURL` in the Havenry repo.

Licence: AGPL-3.0, same as Havenry.
