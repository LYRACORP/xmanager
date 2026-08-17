# Design — XManager web panel

A locked design system for this app. Every page redesign reads this file before
emitting code. Do not regenerate per page — extend or amend this file when the
system needs to grow.

/* Hallmark · pre-emit critique: P4 H4 E4 S4 R4 V3 */

## Genre
modern-minimal

## Macrostructure family

- Marketing pages: none (this repo’s web UI is the ops panel, not a marketing site)
- App pages: Workbench — grouped index rail + command bar, dense work surface, no enrichment
- Auth pages: split, left-biased form (lede left, fields right)
- Content pages: Long Document rhythm for settings and policy forms

## Theme
- `--color-paper`   oklch(98.5% 0.004 250)
- `--color-paper-2` oklch(96.2% 0.006 250)
- `--color-ink`     oklch(24% 0.02 258)
- `--color-ink-2`   oklch(40% 0.018 257)
- `--color-rule`    oklch(89% 0.012 250)
- `--color-accent`  oklch(58% 0.20 256)
- `--color-focus`   oklch(58% 0.20 256)
- `--color-graphite` oklch(22% 0.016 260)
- `--color-danger`  oklch(52% 0.18 25)
- `--color-ok`      oklch(52% 0.12 155)
- `--color-warn`    oklch(68% 0.13 75)

## Typography
- Display: Space Grotesk, weight 600, style normal
- Body:    Inter, weight 400
- Mono:    JetBrains Mono, weight 400
- Display tracking: -0.03em
- Type scale anchor: `--text-display` = clamp(1.75rem, 2vw + 1rem, 2.5rem)

## Spacing
4-point named scale. The values are in `internal/web/static/style.css`. Pages must use named
tokens (`var(--space-md)`), never raw values in new CSS.

## Motion
- Easings: `--ease-out: cubic-bezier(0.16, 1, 0.3, 1)`
- Reveal pattern: none on app pages (composed, not theatrical)
- Reduced-motion fallback: opacity-only, ≤ 150 ms

## Microinteractions stance
- silent success (no celebratory toasts)
- hover delay none; focus delay 0 ms
- command palette is the one signature interaction (⌘K)

## CTA voice
- Primary CTA: solid cobalt fill, 6px radius, names the action (“Sign in”, “Save”)
- Secondary CTA: 1px rule border, transparent fill, same radius

## Per-page allowances
- Marketing pages MAY use enrichment (Tier-A CSS art, Tier-B SVG, etc.).
- App pages MUST NOT use enrichment — function carries the page.
- Content pages: typography only.

## What pages MUST share
- The wordmark “XManager” (roman, display face).
- The accent colour and its placement (≤ 5 % per viewport).
- The display + body + mono fonts.
- The CTA voice (button shape, border-radius, padding rhythm).
- Section heading rhythm: small mono label + display heading, stacked, left-aligned.

## What pages MAY differ on
- Node vs control chrome (index rail vs top bar) — both use the same tokens and ⌘K.
- Auth split vs app workbench.

## Exports

### tokens.css
See `internal/web/static/style.css` `:root`.
