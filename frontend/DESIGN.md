# JournAI Frontend Design Language

Source of truth for visual + motion + interaction decisions. Update this file when the design system changes; do not let code and this doc drift.

## North star

**Warm, not utilitarian.** The journal should feel like *ink on paper* — a cream sheet under desk lamp light, or a leather notebook in a candlelit study — not a SaaS dashboard. The default dark mode should feel like dim warm wood, never blue-slate Slack-night. The light mode should feel like aged paper, never gray-100.

Cool primary (ink-violet) against warm neutrals (paper) is the central tension: *cool ink on warm page*. Don't unify them.

## Stack

- React 18 + Vite + Tailwind 3 (CSS-var driven HSL tokens)
- shadcn-style primitives (real CVA copies, not stubs) under `src/components/ui/`
- `motion` 12.x for animations (framer-motion successor)
- `next-themes` for light/dark with `attribute="class"`, `disableTransitionOnChange`, `defaultTheme="system"`
- `sonner` for toasts; Radix for Dialog/AlertDialog/Select/Tabs/Separator
- Self-hosted fonts via `@fontsource(-variable)` so the PWA works offline

## Tokens

All HSL triplets. Defined in `frontend/src/styles/index.css`. Each palette declares both modes via `:root[data-palette="<name>"]` (light) and `.dark[data-palette="<name>"]` (dark). The unqualified `:root` and `.dark` blocks are aliased to **paper** so the default palette works without the attribute being set.

Shadows derive their tint from a per-palette `--shadow-tint` var, so a single shadow declaration covers every palette.

### Light — paper / cream

| Token | Value | Intent |
|---|---|---|
| `--background` | `39 38% 96%` | Warm cream paper. Never `#FFF`. |
| `--foreground` | `30 12% 14%` | Soft charcoal — long-write friendly. |
| `--muted` | `36 22% 92%` | Hover wash, secondary surfaces. |
| `--muted-foreground` | `30 8% 42%` | Pencil grey. |
| `--border` | `36 18% 84%` | Visible without harshness. |
| `--input` | `36 22% 90%` | Recessed from page. |
| `--ring` | `252 70% 56%` | Focus halo. |
| `--primary` | `252 70% 50%` | Ink-violet ("journal pen"). |
| `--primary-foreground` | `39 38% 98%` | |
| `--secondary` | `36 24% 88%` | Low-emphasis surfaces. |
| `--secondary-foreground` | `30 12% 14%` | |
| `--accent` | `18 70% 52%` | Terracotta — margin pen, today-pill, sparing emphasis. |
| `--accent-foreground` | `39 38% 98%` | |
| `--destructive` | `0 65% 48%` | |
| `--card` | `39 44% 98%` | Fresh sheet on the desk — brighter than page. |
| `--popover` | `39 44% 98%` | |
| `--success` | `152 50% 38%` | Moss. |
| `--warning` | `36 84% 44%` | Amber. |

### Dark — warm near-black, ember undertone

Hue 30° (warm) for neutrals — *not* the previous 240° cool slate. Foreground also warmed.

| Token | Value | Intent |
|---|---|---|
| `--background` | `30 8% 6%` | Warm near-black. |
| `--foreground` | `36 12% 92%` | Warm white. |
| `--muted` | `30 6% 11%` | |
| `--muted-foreground` | `36 6% 62%` | |
| `--border` | `30 5% 16%` | |
| `--input` | `30 5% 14%` | |
| `--ring` | `252 90% 72%` | |
| `--primary` | `252 88% 72%` | Ink-violet, saturation reduced from 100%. |
| `--primary-foreground` | `30 10% 8%` | |
| `--secondary` | `30 5% 13%` | |
| `--secondary-foreground` | `36 12% 92%` | |
| `--accent` | `18 78% 64%` | Terracotta, lifted for dark. |
| `--accent-foreground` | `30 10% 8%` | |
| `--destructive` | `0 70% 60%` | |
| `--card` | `30 7% 9%` | |
| `--popover` | `30 7% 9%` | |
| `--success` | `152 55% 56%` | |
| `--warning` | `36 84% 62%` | |

### Palettes

The user picks a palette in **Settings → General → Appearance**. Selection is persisted to `localStorage["journai.palette"]` and applied to `<html data-palette="...">` by an anti-flash script in `index.html` *before* React hydrates, so first paint matches.

Both halves — light/dark mode and palette — live on `<html>`: `class="dark"` (driven by next-themes) crosses with `data-palette="..."` (driven by `usePalette()` in `src/lib/palette.ts`). Every palette ships both halves.

| Palette | Page | Ink (primary) | Accent | Feel |
|---|---|---|---|---|
| **paper** *(default)* | warm cream `39 38% 96%` | ink-violet `252 70% 50%` | terracotta `18 70% 52%` | canonical journal — paper under lamplight |
| **ember** | peach cream `24 44% 95%` | burnt orange `22 80% 44%` | deep teal `190 60% 36%` | embers in candlelight — fired earth, warm hearth |
| **forest** | sage cream `80 24% 94%` | deep moss `152 55% 30%` | cranberry `352 60% 46%` | leather notebook in a study |
| **ocean** | sea-foam `200 38% 95%` | deep teal `195 75% 32%` | sun gold `42 90% 46%` | calm, breezy |
| **slate** | cool gray `220 16% 96%` | ink-violet `252 70% 50%` | magenta `330 72% 52%` | modern, saturated — the "anti-paper" option |

Accents are deliberately spread around the wheel (18° / 190° / 352° / 42° / 330°) so palettes read as visibly distinct identities even on small accented elements (question heading bars, link underlines, the `text-accent` mood label). Ember is the only palette where the *primary* itself is a warm orange (22°) — every other palette uses a cool/neutral primary against a warm accent.

**Primary vs accent in components.** Use `--primary` for the dominant interactive ink — buttons, slider Range/Thumb, focus ring. Use `--accent` for sparing flourishes (margin-pen left bars, link underlines, today-pill). Don't lean on accent for large filled surfaces; it's the spice, not the dish.

Each palette also defines `--theme-color` (an `R G B` triplet) which `syncThemeColorMeta()` reads and writes into the single `<meta name="theme-color">` on every palette/mode change. Old dual `media`-keyed metas were replaced because we need the chrome to follow the *user-selected* palette, not the OS preference alone.

### Radius

CSS-var scale: `--radius-sm: 0.375rem`, `--radius-md: 0.5rem`, `--radius-lg: 0.75rem` (default), `--radius-xl: 1rem`, `--radius-2xl: 1.5rem` (auth hero).

### Shadow (theme-aware)

- **Light** = real drop shadows (paper has weight). `--shadow-sm`, `--shadow-md`, `--shadow-lg` use foreground hue at low alpha.
- **Dark** = inset top-edge highlight + low ambient — sells elevation on OLED without a visible drop. Same `--shadow-*` token names per theme so `shadow-md` resolves correctly via Tailwind extend.

## Typography

Three families, all self-hosted:

| Family | Use |
|---|---|
| **Geist Variable** (`@fontsource-variable/geist`) | Primary sans: body, UI, prompts. Humanist, sharp at small sizes. |
| **Instrument Serif** (`@fontsource/instrument-serif`) | Display serif: h1, dates, wordmark, auth titles. Sells "paper". |
| **JetBrains Mono Variable** (`@fontsource-variable/jetbrains-mono`) | Clock, save-pill, tabular numbers. |

Tailwind `fontFamily: { sans, serif, mono }`. Preload Geist 400 + 500 woff2 in `index.html`.

Body `font-feature-settings: "ss01", "cv01"` (Geist single-storey g). `.tabular-nums` uses `"tnum"`. Drop the old Inter `cv11/ss01/ss02` (silently no-op on Geist).

### Type scale

| Name | Size / leading / tracking | Family |
|---|---|---|
| `display` | 3rem / 1.05 / -0.03em | serif |
| `h1` | 2rem / 1.1 / -0.02em | serif (history-detail, Today's date) |
| `h2` | 1.5rem / 1.2 / -0.015em | sans semibold |
| `h3` | 1.125rem / 1.3 / -0.01em | sans medium (card title, prompt) |
| `body` | 1rem / **1.65** / 0 | sans (entry textareas — leading bumped because users *write* here) |
| `body-prose` | 1.0625rem / 1.75 / 0.005em | sans (markdown read-mode) |
| `small` | 0.875rem / 1.5 / 0 | sans |
| `caption` | 0.75rem / 1.4 / 0.04em uppercase | sans medium (eyebrow labels) |

## Motion

Library: **`motion` 12.x**. Primitives in `src/lib/motion.ts`.

### Easing constants

```ts
easeStandard    = [0.32, 0.72, 0, 1]   // content entering view (Apple-natural)
easeEmphasized  = [0.2, 0, 0, 1]       // page transitions
easeExit        = [0.4, 0, 1, 1]       // things leaving
springTactile   = { type: 'spring', stiffness: 380, damping: 30 }
```

### Pattern bank

- **Page enter/exit** — `<AnimatePresence mode="wait">` keyed on pathname. Enter `opacity 0→1, y 8→0` 280ms `easeStandard`. Exit `opacity 1→0` 160ms `easeExit`.
- **List stagger** — `opacity 0→1, y 6→0` 220ms per item, `staggerChildren: 0.04`, `delayChildren: 0.05`. Cap at 8 items.
- **Save-pill (StatusPill)** — `motion.span` with `layout` enabled; FLIPs width between states. Color cross-fades 200ms. Dot pulses on `dirty→saving`.
- **Hover elevation** — `whileHover={{ y: -1, boxShadow: shadow-md }}`, 150ms. Gated behind `@media (hover: hover) and (pointer: fine)`.
- **Reorder (questions list)** — `motion.li layoutId={q.id}` slides on cache reorder.

### Reduced motion

Every variant goes through `useReducedMotion()` from `motion/react`. Falls back to opacity-only — no y-translate, no stagger. Sonner respects natively.

### Theme switch

`disableTransitionOnChange` is required on `ThemeProvider` — adds a temporary `*{transition:none!important}` so the swap is instant, not a 600ms whole-app crossfade.

## App shell

- **Desktop (md+)** — persistent left sidebar (12-16rem). Wordmark (Instrument Serif italic) at top; NavLinks with lucide icons (`PenLine`/`History`/`MessageSquare`/`Settings`); footer = email + theme toggle + sign-out. Scales for Phase 4-7 surfaces (one row per top-level route).
- **Mobile (<md)** — bottom tab bar, fixed, `pb-[env(safe-area-inset-bottom)]`. 4 NavLinks (icon + tiny label). Active state via `motion.div` with `layoutId="bottom-nav-pill"`. Slim sticky top header (page title + theme toggle) collapses on scroll-down.
- **Auth layout** — separate minimal layout `AuthLayout` for `/auth/login`, `/auth/verify`, `/health`. Centered `min-h-svh grid place-items-center`. Brand + theme toggle only.

## Interaction patterns

- **Save-on-blur** — primary save trigger for entry textareas. Cache-update IS the feedback (optimistic). Drop "Saving…/Saved/Unsaved" inline text — only show a `<Loader2 />` after >300ms via `useDebouncedFlag`. Errors → Sonner toast + rollback.
- **No browser `confirm()`** — all destructive confirms use `<AlertDialog>` from Radix.
- **No bare `<select>`** — use the Radix Select primitive (timezone picker, etc.) with type-to-filter when list is large.
- **Mobile gestures** (touch only):
  - **Swipe between days** — Today swipe-right → yesterday; History swipe-left/right → next/prev. iOS dead-zone: ignore drags starting in leftmost 20px.
  - **Pull-to-refresh** — Today + History list. Only attaches when `window.scrollY === 0`.
  - **Prefetch on drag-start** — fire `queryClient.prefetchQuery` for both adjacent dates so whichever direction commits is warm.

## Performance

- **Route code-splitting** via `React.lazy` for all 6 routes.
- **Vite manualChunks**: `react-vendor`, `query-vendor`, `motion-vendor`, `radix-vendor`, `icons`, `markdown`.
- **AuthGate prefetch** — `useMe()` hoisted to shell; once `me.data` lands, prefetch questions + entries + entry-dates in the same tick.
- **Workbox runtime caching** in `src/sw/push-handler.ts`:
  - **All `/api/*` routes are NetworkOnly.** Earlier we used SWR for `/api/questions` and `/api/entries/dates`, but the post-mutation refetch (TanStack `invalidateQueries` in `onSettled`) read the stale SW cache and reverted optimistic updates — required a second click to "stick." Until we add explicit cache invalidation on POST/PATCH/DELETE, NetworkOnly is the only correct strategy for any endpoint we write to from the same client.
  - Offline read support for `/api/questions` and `/api/entries/dates` is Phase 5+ work; when added, register mutation-method matchers that `caches.delete(...)` the corresponding cache on success.
- **TanStack staleTime**: questions 5min, entry-dates 60s, entries-today 30s, **entries-past `Infinity`** (history immutable from cache POV; explicit invalidates on edit handle the rest), `gcTime` 30min on entries.
- **`index.html` hints**: preconnect + dns-prefetch to API origin; preload Geist 400/500 woff2; preload icon SVG.

## A11y

- All interactive elements keyboard-focusable with visible ring.
- Aria-labels on icon-only buttons.
- Verify accent contrast at AA — terracotta `18 70% 52%` on cream is ~4.0:1 (passes large-text only). Bump to `18 75% 42%` if used for small body text.
- Reduced-motion path verified per pattern.
- iOS PWA chrome via two `<meta theme-color>` with `media="(prefers-color-scheme:...)"`.

## Anti-patterns

Don't:
- Hardcode any color value in components — all colors flow through CSS vars (`bg-background`, `text-foreground`, `border-border`, etc.) so palette switches stay coherent. The only colors in JS are the palette-picker swatches in `src/lib/palette.ts`.
- Add another palette without a clear identity. Each existing palette has a distinct *page hue* (warm cream / peach cream / sage / sea-foam / cool gray) plus a deliberate ink/accent pairing — don't ship a near-duplicate.
- Animate via `style={{ background: ... }}` through motion — always go through CSS vars so theme-swap remains instant.
- Re-render the shell on theme toggle — colocate `useTheme()` into the toggle, not the shell.
- Add inline status text on save (`"Saving…"/"Saved"/"Unsaved"`) — cache update is the feedback; reserve the textual indicator for the >300ms slow-network path.
- Reach for `experimental_prefetchInRender` (double-fires under React 18 strict mode).
- Add `vaul` Sheet or `cmdk` palette without a clear product reason — keep dep count down.

## Push reminders (Phase 5)

- The reminders surface lives in **Settings → Notifications** alongside the existing reminder-time picker. There is no separate "Notifications" page; subscription is configuration, not navigation.
- Subscribe button is per-device by design — phones and laptops are separate `push_subscriptions` rows, and the "Send test notification" button always targets the device the user clicked from. The device list under the button shows other endpoints with the rough "Chrome on macOS · last seen 3d ago" label, so the user can audit and unsubscribe stale ones from this card.
- iOS gating is explicit: when we detect iPhone/iPad without `display-mode: standalone`, the entire card is replaced with the "Add to Home Screen first" copy. This isn't a hint at the bottom — it's the whole content, because the subscribe button physically cannot work, and offering it would leak frustration.
- The custom service worker (`src/sw/push-handler.ts`) handles `push`, `notificationclick`, and `pushsubscriptionchange`. The third one is the iOS-reliability linchpin — the SW silently re-subscribes against `/api/push/vapid-public-key` whenever the OS rotates the endpoint, so users don't lose reminders after a reboot.
- Notification visuals match the OS, not our palette — title `JournAI`, icon `/pwa-192.png`. No custom styling because the OS overrides everything anyway.

## Open / deferred

- `vaul` Sheet — deferred. HistoryView's date rail works inline.
- `cmdk` command palette — deferred until power-user flows demand it.
- Decorative paper-grain SVG — skipped; cream + serif carry it.
- React Compiler — deferred until React 19 upgrade (compiler targets 19+).
