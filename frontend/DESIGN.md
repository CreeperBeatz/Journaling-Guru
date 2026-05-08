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
| `--paper-sheet` | `39 50% 99%` | Single-sheet surface (PaperPage) — one notch brighter than `--card`. |
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
| `--paper-sheet` | `30 8% 11%` | Single-sheet surface (PaperPage), one notch brighter than `--card`. |
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
| **forest** | honey sand `50 26% 94%` | deep moss `152 55% 30%` | cranberry `352 60% 46%` | leather notebook in a warm study |
| **ocean** | warm sand `32 32% 94%` | deep teal `195 75% 32%` | sun gold `42 90% 46%` | beach light, calm sea |
| **slate** | warm clay `36 14% 93%` | ink-violet `252 70% 50%` | magenta `330 72% 52%` | modern, saturated — paper with sharper teeth |

Every palette is warm — the *page* hue lives in 24°-50° (peach / cream / sand / clay), never in cool 180°-260° space. Identity comes from the primary/accent pairing, not from a cold page. Accents are spread around the wheel (18° / 190° / 352° / 42° / 330°) so palettes read as distinct on small accented elements (question heading bars, link underlines, the `text-accent` mood label). Ember is the only palette where the *primary* itself is a warm orange (22°); every other palette uses a cool primary (moss / teal / ink-violet) against the warm page — the canonical *cool ink on warm page* tension.

Dark modes likewise unify on warm near-black (hue 28°-36°), never the cool 215°-222° range. Identity in dark mode comes from primary/accent only — the page is always dim warm wood.

**Primary vs accent in components.** Use `--primary` for the dominant interactive ink — buttons, slider Range/Thumb, focus ring. Use `--accent` for sparing flourishes (margin-pen left bars, link underlines, today-pill). Don't lean on accent for large filled surfaces; it's the spice, not the dish.

Each palette also defines `--theme-color` (an `R G B` triplet) which `syncThemeColorMeta()` reads and writes into the single `<meta name="theme-color">` on every palette/mode change. Old dual `media`-keyed metas were replaced because we need the chrome to follow the *user-selected* palette, not the OS preference alone.

### Radius

CSS-var scale: `--radius-sm: 0.375rem`, `--radius-md: 0.5rem`, `--radius-lg: 0.75rem` (default), `--radius-xl: 1rem`, `--radius-2xl: 1.5rem` (auth hero).

### Shadow (theme-aware)

- **Light** = real drop shadows (paper has weight). `--shadow-sm`, `--shadow-md`, `--shadow-lg` use foreground hue at low alpha.
- **Dark** = inset top-edge highlight + low ambient — sells elevation on OLED without a visible drop. Same `--shadow-*` token names per theme so `shadow-md` resolves correctly via Tailwind extend.

### Heat (history grid)

The history heatmap derives its ramp from `--primary` rather than declaring per-palette explicit steps — so the grid re-skins automatically on palette switch.

| Token | Value | Intent |
|---|---|---|
| `--heat-empty` | `hsl(var(--muted))` | No entry on that day. |
| `--heat-l1` | `hsl(var(--primary) / 0.18)` | Short streak (1–2 questions answered). |
| `--heat-l2` | `hsl(var(--primary) / 0.40)` | Mid streak (3–5 answered or chat session ≥3 turns). |
| `--heat-l3` | `hsl(var(--primary) / 0.65)` | Deep streak (full answers + chat). |
| `--heat-l4` | `hsl(var(--primary) / 0.90)` | Deep streak (consecutive deep days). |
| `--heat-mood` | `hsl(var(--accent))` | Inset 1px ring on cells where `daily_inputs.mood >= 4`. |

Cells use `--heat-l*` for fill and `--heat-mood` for the optional mood-up ring. Don't hardcode level colors per palette.

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
- **CardStack advance** — `motion.div` with `layoutId="manual-card"`. Next-card enter from `x: 24, opacity: 0`; current exits `x: -24, opacity: 0`. `springTactile`. Swipe-back inverts direction. Reduced-motion: opacity-only crossfade, no x-translate.
- **Q7 morph (filling → complete)** — `<AnimatePresence mode="wait">` keyed on `phase: 'filling' | 'complete'`. CardStack exits `opacity 1→0, y 0→-12` 200ms `easeExit`; PaperPage enters `opacity 0→1, y 8→0` 360ms `easeEmphasized`. The serif date headline animates `letter-spacing` from `-0.01em → -0.03em` over the same window — that's the visual "settle." Small celebratory micro-moment, **never** confetti or particles. Reduced-motion: opacity-only, no y, no letter-spacing animation.

### Reduced motion

Every variant goes through `useReducedMotion()` from `motion/react`. Falls back to opacity-only — no y-translate, no stagger. Sonner respects natively.

### Theme switch

`disableTransitionOnChange` is required on `ThemeProvider` — adds a temporary `*{transition:none!important}` so the swap is instant, not a 600ms whole-app crossfade.

## App shell

The IA is **four top-level surfaces**: `chats · history · summary · settings`. There is no "Today" — the primary surface is a conversation, and today's chat is the default landing of `chats`. Past sessions are not surfaced as a list; they appear inline on `/history/:date` via `<HistoryChatTranscript>`.

| Route | Label | Icon (lucide) |
|---|---|---|
| `/` | chats | `MessageSquare` |
| `/history` | history | `CalendarDays` |
| `/summary` | summary | `Sparkles` |
| `/settings` | settings | `Settings` |

Legacy redirects: `/today → /`, `/summaries → /summary`, `/summaries/:id → /summary`. Bookmarks must not 404.

- **Desktop (md+)** — persistent left sidebar (12-16rem). Wordmark (Instrument Serif italic) at top; NavLinks with the four icons above; footer = email + theme toggle + sign-out. One row per top-level route.
- **Mobile (<md)** — bottom tab bar, fixed, `pb-[env(safe-area-inset-bottom)]`. 4 NavLinks (icon + tiny label, in the order above). Active state via `motion.div` with `layoutId="bottom-nav-pill"`. Slim sticky top header (page title + theme toggle) collapses on scroll-down.
- **Auth layout** — separate minimal layout `AuthLayout` for `/auth/login`, `/auth/verify`, `/health`. Centered `min-h-svh grid place-items-center`. Brand + theme toggle only.

## Interaction patterns

- **Save-on-blur** — primary save trigger for entry textareas. Cache-update IS the feedback (optimistic). Drop "Saving…/Saved/Unsaved" inline text — only show a `<Loader2 />` after >300ms via `useDebouncedFlag`. Errors → Sonner toast + rollback.
- **No browser `confirm()`** — all destructive confirms use `<AlertDialog>` from Radix.
- **No bare `<select>`** — use the Radix Select primitive (timezone picker, etc.) with type-to-filter when list is large.
- **Mobile gestures** (touch only):
  - **Swipe between days** — Today swipe-right → yesterday; History swipe-left/right → next/prev. iOS dead-zone: ignore drags starting in leftmost 20px.
  - **Pull-to-refresh** — Today + History list. Only attaches when `window.scrollY === 0`.
  - **Prefetch on drag-start** — fire `queryClient.prefetchQuery` for both adjacent dates so whichever direction commits is warm.

## Manual flow (Phase 4 — cards → paper)

The Manual mode of the chats surface is **not** a stack of textareas. It has two states with a single morph between them.

### Filling state — `<CardStack>`

One slot per card, full focus. Lives in `src/components/ui/card-stack.tsx` (slot-based; lifted out of `features/journal` because PaperPage reuses the question shape).

- **Slot order** — `Mood → Emotions → ...questions`. Mood and Emotions are not "questions" — they're the existing `daily_inputs` mood/emotions fields rendered as cards so the check-in is a single linear flow. Notes (third field on `daily_inputs`) is **not** a card — it stays on the PaperPage / DailyInputs card in the complete state, since most days users don't write notes.
- **Layout** — single card visible, `min-h-[60svh]`, content vertically centered. The card itself is a `<Card>` on `bg-card`, with the question prompt as `h2` serif, optional eyebrow `caption`, and a single input control sized to the slot's `kind`:
  - Mood card: large tabular-nums `display`-size number + 1–10 slider (`<Slider ticks>`); skip leaves mood unset (null).
  - Emotions card: `<Textarea rows={6}>`, free text. Server-side classifier runs async; not echoed.
  - `short answer` → `<Input>` (Phase-7 backend kind).
  - `sentence` → `<Textarea rows={6}>` (default for questions today).
  - `sentence + chips` → `<Textarea>` with a chip row below.
  - `word` → `<Input>` with `text-2xl serif`.
  - `name + why` → two-row group (`<Input>` + `<Textarea>`).
- **Advance on submit** — `↵` for short answers; `cmd/ctrl+↵` for textareas. The card commits its value to the cache + POSTs *on advance*, not on blur. (PaperPage owns blur saves.)
- **Back** — swipe-right (touch only, with the iOS leftmost-20px dead-zone) and a soft `<ChevronLeft>` button revisit the previous card. Already-filled cards prefill from cache.
- **Progress** — a single thin progress bar `bg-accent` at the top of the card, width `= (index + 1) / total`. No "Question 3 of 7" copy — the bar carries it.
- **Skip-to-end** — a small `Show full page →` affordance lives next to the progress bar. Clicking it fires `onComplete()` directly, jumping the surface to PaperPage without walking through every card. Reading users + return-day users want this; the cards are the on-ramp, not a forced corridor.
- **Skip card** — no per-card skip. Empty submit advances with no value; empty mood / emotions / answer don't count toward the streak level (see History).

### Completion state — `<PaperPage>`

When the last card submits, the surface morphs to a single scrollable sheet with all answers visible and inline-editable. Same primitive is reused on `/history/:date` and the weekly letter under `/summary`.

- **Surface** — `bg-paper-sheet` (one notch brighter than `--card`), generous left/right padding, max-width `prose` so reading length is comfortable. Single `shadow-md` lift in light mode; flat in dark.
- **Header** — serif `h1` date, `text-accent` left margin-bar (4px), small `caption`-eyebrow ("Today" / "Tuesday, 2 weeks ago" / "A letter from your guru").
- **Blocks** — for an entry-day variant, an array of `{ eyebrow: prompt, body: answer }`. Each block renders prompt as `caption` uppercase eyebrow + `body-prose` answer. Click-to-edit converts the body into the same input control the CardStack used. **Save-on-blur** here; same debounced-flag pattern as old `<DailyEntry>`.
- **Variants**:
  - `variant="entry"` — used by Today completion + History detail. Blocks are question/answer pairs, editable.
  - `variant="letter"` — used by WeeklyLetter. Single prose body in `body-prose`, read-only, with jump-to-day chips at the foot.
- **Save semantics** — entries: save-on-blur, optimistic, status pill in the page header. Letters: read-only.

### Q7 transition

See Motion → Q7 morph above. The transition fires when the user submits the last card and `useEntries(today).every(answered)` becomes true. If the user navigates back to Manual after the transition, they land on PaperPage directly (filling state is only entered when at least one question is unanswered).

### Anti-patterns

Don't:
- Keep the old textarea-stack manual surface alongside CardStack/PaperPage. There's exactly one Manual flow.
- Trigger the Q7 morph mid-stack (e.g., when only some questions are answered). It fires once, on the final card's submit.
- Save on blur inside CardStack. Cards commit on advance — blur in the middle of typing should not POST.
- Hardcode confetti, particle effects, or anything celebratory beyond the spec'd morph. The serif settling-in IS the moment.

## History (Phase 4 — heatmap-first)

`/history` opens to a **calendar heatmap**, not a date rail. The mental model is "where am I in the streak?" before "what did I write on day X?"

### Page composition

```
<header>            → StreakBadge ("14 day streak") + view toggle (year · month · week)
<HeatGrid>          → primary view, fills width
<RecentEntries>     → list below the grid (date · mood words · "5/7 answered")
```

`/history/:date` renders `<PaperPage variant="entry">` for that day, plus `<HistoryDailyInputs>` (mood/emotions/notes) and `<HistoryChatTranscript>` below. Swipe-between-days from old `HistoryView` carries over: swipe-left/right → next/prev `local_date`, with prefetch on drag-start.

### `<HeatGrid>`

Lives in `src/components/ui/heat-grid.tsx`.

- **Props** — `cells: { date: string; level: 0 | 1 | 2 | 3 | 4; moodUp: boolean }[]`, `view: 'year' | 'month' | 'week'`, `onSelect(date)`.
- **Cell** — rounded-sm square, `bg-[hsl(var(--heat-l<level>))]` (or `--heat-empty` for level 0). When `moodUp`, add an inset 1px ring `ring-inset ring-1 ring-[hsl(var(--heat-mood))]`.
- **Sizing** — year view: 53 cols × 7 rows, ~10px cells, gap 2px. Month view: ~5–6 rows × 7 cols, ~28px cells, gap 4px. Week view: 1 row × 7 cols, ~48px cells.
- **Hover/focus** — `whileHover={{ scale: 1.06 }}` (gated behind `(hover: hover)`); focus ring on keyboard.
- **Click** — `onSelect(date)` → `navigate('/history/' + date)`.
- **A11y** — each cell `role="button"`, `aria-label="2026-04-12, 5 of 7 answered"`. The grid is a `role="grid"` with date columns/rows wired up.

### Streak level rule

Computed client-side from a single endpoint (`GET /api/history/heatmap?from&to`):

| Level | Condition |
|---|---|
| 0 | No entries that day. |
| 1 | 1–2 questions answered, no chat session. |
| 2 | 3–5 answered **or** chat session with ≥3 turns. |
| 3 | 6+ answered AND chat session with ≥3 turns. |
| 4 | Level-3 day inside a run of ≥3 consecutive level-3 days. |
| `moodUp` | `daily_inputs.mood_score >= 7` (top tertile of the 1-10 scale). Independent of level — applies as the inset ring. |

Streak counter (`<StreakBadge>`) = consecutive days back from today with `level >= 1`. Renders in the page header, `font-mono tabular-nums`, accent number on `bg-accent/10` rounded-full pill.

### Recent entries list

A simple `<ul>` below the grid, latest 14 days that have `level >= 1`. Each row: `caption` date on the left, mood-word(s) from `daily_inputs.emotions` in `text-accent`, "5/7 answered" in `text-muted-foreground` on the right. Tapping → `/history/:date`. Stagger-in via the standard list pattern.

### Removed

The old "By question" view is **gone from History**. It moves to `/summary` → ByQuestion tab. Don't render `HistoryByQuestion` anywhere on `/history`.

### Anti-patterns

Don't:
- Color cells with hardcoded hex or per-palette steps. The `--heat-l*` tokens derive from `--primary` so palette switches re-skin automatically.
- Show absolute counts inside cells. The cell IS the count.
- Render a date rail above the grid in year view — the grid carries the time axis.

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
- Add another palette without a clear identity. Each existing palette has a warm page hue (cream / peach / sand / clay, all 24°-50°) plus a deliberate ink/accent pairing — don't ship a near-duplicate, and don't ship one with a cool (180°-260°) page.
- Animate via `style={{ background: ... }}` through motion — always go through CSS vars so theme-swap remains instant.
- Re-render the shell on theme toggle — colocate `useTheme()` into the toggle, not the shell.
- Add inline status text on save (`"Saving…"/"Saved"/"Unsaved"`) — cache update is the feedback; reserve the textual indicator for the >300ms slow-network path.
- Reach for `experimental_prefetchInRender` (double-fires under React 18 strict mode).
- Add `vaul` Sheet or `cmdk` palette without a clear product reason — keep dep count down.

## Chat (Phase 6a)

The chats surface (`/`) is a Tabs dispatcher with three modes — **Manual**, **Chat**, **Talk**. Chat is the default; Talk is a `disabled` tab with a `<Badge>soon</Badge>` reserved for Phase 6b voice. (The route was previously called "Today"; see App shell for the IA rename.) The Manual mode is specced separately in the **Manual flow** section above — it is not a stack of textareas.

### Mode resolution + persistence

Default mode is resolved in this priority order:

1. `?mode=` URL search param — deep-links and reload-after-toggle stay stable.
2. `localStorage["journai.todayMode"]` — remember-my-last-tab.
3. `chat` — the v6a thesis: engagement first, data entry second.

Tab toggles write back to both URL (replace, no history entry) and localStorage. SwipeNavigator is unwrapped from chat mode — horizontal drag would fight the message-list scroll on touch devices.

### Conversation surface

- **Bubble alignment**: user right-aligned, assistant left-aligned. `max-w-[85%] sm:max-w-[75%]`. User uses `bg-primary/8 text-foreground`; assistant uses `border border-border/60 bg-card text-card-foreground`. Soft border on assistant; no border on user. Both `rounded-2xl px-4 py-3 text-base leading-relaxed`.
- **No timestamps, no avatars**, no per-bubble metadata. This is a reflection space, not a chat app — chrome breaks the ink-on-paper intent.
- **Auto-stick to bottom**: `MessageList` calls `scrollIntoView({behavior:"smooth", block:"end"})` on every messages-length or partial-text change. We deliberately don't fight users who scroll up — they'll lose their place if a new chunk arrives. Acceptable trade for v1.

### Streaming render

The streaming itself is the effect — no synthetic typewriter delay layered on top.

- Each chunk batch arrives via SSE → `partial` state grows → React re-renders `<StreamingMessage>` with the running string.
- The bubble itself uses `motion.div` with `initial={{opacity:0, y:6}}, animate={{opacity:1, y:0}}, transition={{duration:0.18, ease:easeStandard}}`.
- A blinking caret (`<span className="motion-safe:animate-pulse">`) marks the live tail.
- A 3-dot terracotta loader pulses below the bubble while streaming; opacity-stagger via three `motion.span` with `delay: i * 0.15`. Reduced-motion: replace with the static text "thinking…".
- When the `done` SSE frame lands, `useStreamingChat` sets `status='done'`, clears `partial`, and invalidates `chatSessionKey()`. The persisted assistant row replaces the streaming bubble on the next render.

### Coverage chips (post-turn classifier)

A horizontally-scrollable pill row sits below the sticky Today bar when the user has any active questions. Each pill = one question, truncated to 32 chars + ellipsis (full prompt in `title=` for tooltip).

- **Inactive** (default): `bg-muted text-muted-foreground border border-border/60`.
- **Active** (the post-turn classifier marked this `question_id` covered): `bg-accent/15 text-accent border border-accent/30`. Transition via `springTactile` for a tactile-but-not-bouncy fill.
- **Compact mode** (driven by the sticky-bar `data-stuck` signal — see below): each chip collapses to a 10px dot. `bg-accent` when lit, `bg-border` when not. Tooltips still expose the full prompt; `aria-label` carries the covered state for screen readers.
- Read-only — tapping does nothing today.
- Source of truth is `chat_sessions.covered_question_ids` (a backend column written by `chat.Classify` after each assistant turn). The SSE `coverage_update` frame patches the same query-cache entry mid-stream so dots light up live; a page refresh re-renders from the persisted column. The previous inline `mark_topic_covered` tool was removed in favor of this dedicated post-turn pass — it can review the full turn in context instead of marking optimistically mid-reply.

### Phase-driven UI

Session phase ENUM: `greeting | exploring | wrapping_up | finalized | abandoned`. The chat header shows a phase label ("Starting up", "Reflecting", "Wrapping up", "Today's chat is closed"). Composer placeholder swaps per phase ("Type your reply…", "Say something…", "One last thing on your mind?").

When the model emits `propose_wrap_up`, the SSE handler advances the session to `wrapping_up` and emits a `phase` frame. The FE caches the new phase immediately, and `<WrapUpAffordance>` slides in below the composer with a primary "I'm done" button.

### Crisis card

When the safety regex (server-side, `chat/safety.go`) hits a user message, the streaming handler short-circuits and emits a single `crisis` SSE frame. The FE swaps the message-list footer for `<CrisisCard>` — an in-flow `<Card>` (NOT a Dialog; interrupting the message list with a modal is jarring), `border-destructive/40 bg-destructive/5`, with hand-curated copy: 988, Crisis Text Line, link to `/resources`.

Two affordances: **Get support** (primary, opens the resources page in a new tab) and **I want to keep talking** (ghost, dismisses the card). The user's message stays in the transcript either way — context is preserved. No LLM call is ever made on a crisis-flagged message; the card is the only response.

The `/resources` page itself is static, public (no auth gate), hand-curated. Linked from CrisisCard and Settings.

### Sticky chats header (`DailyEntry` — file name preserved; surface is now `chats`)

The date header + mode tabs + (in chat mode) coverage chips are wrapped in a sticky group at the top of `/today`. As soon as the user scrolls past its natural position the bar collapses from a multi-line display block (`Today` eyebrow, serif `<h1>`, `currentTime · timezone · day-start` line) to a thin one-line strip with the short date + tabs + status pill. Coverage chips stack as a second sticky strip directly below, switching to dots-only in the stuck state.

- **Glass tokens**: `bg-background/85 backdrop-blur-md`, mirroring AppShell's mobile header (`AppShell.tsx`) and the Settings sticky save bar. Border-bottom is transparent in flow and `border-border/60` when stuck — the line is the visual cue that the bar has detached.
- **Layering**: AppShell's mobile header is `z-30`; the sticky Today bar is `z-20` so it parks below it (`top-[3.5rem] md:top-0`). The coverage strip is `z-10` and pins below the Today bar at `top-[6.25rem] md:top-[2.75rem]`.
- **Stuck detection**: a tiny inline `useIsStuck` hook reads the bar's `getBoundingClientRect().top` on scroll/resize and flips a state when the rect reaches the viewport's sticky offset. Both the bar's collapse and the coverage chips' compact mode share that single signal (passed as `coverageCompact` to `ChatPanel`) so the two stickies always transition in sync.
- **Bleed-out**: `-mx-4 md:-mx-8` (matches AppShell main padding) so the glass strip extends to the edges of the constrained content frame.
- **Pill slot**: between the header block and `<TabsList>`, `inline-flex shrink-0` so it fits inline with the row in stuck mode and `self-end` (right-aligned) above the tabs in flow mode.

### Background extraction pill

Clicking "Update check-in" no longer replaces the chat surface — the extraction worker runs in the background and the chat stays interactive. Status surfaces as a small pill in the sticky Today bar.

- **Pending / running**: `bg-accent/15 text-accent` rounded-full pill with a `Loader2` spinner + "Updating check-in…". `role="status" aria-live="polite"` so a screen reader announces the change without interrupting.
- **Failed**: `bg-destructive/10 text-destructive border border-destructive/40` button — clicking re-fires `useFinalizeChat` (idempotent on the backend; the `chat_extraction_jobs.session_id` UNIQUE constraint deduplicates within the same minute). The error message lives in `title=` for tooltip discovery.
- **Idle / completed**: pill is absent. The success toast in `useExtractionStatus` ("Check-in updated") is the completion signal.
- The hook is called in `DailyEntry` (not `ChatPanel`) so the polling effect — and the toast it fires on `completed` / `failed` — runs exactly once. ChatPanel's "Update check-in" button still triggers the same mutation; both observers converge on the cached session row.
- New chat messages typed during a background extraction are simply not part of the snapshot the worker just consumed. They land in the next update.

### History view integration

`<HistoryChatTranscript localDate={...} />` slots between `<HistoryDailyInputs>` and the entries Card on `/history/:date`. Read-only; default-collapsed `<details>` so the page stays uncluttered. Header shows turn count + "auto-filled into the check-in" / "draft" status. Renders nothing when no chat session exists for that day.

### Anti-patterns

Don't:
- Add a per-token typewriter delay. The SSE stream's natural rhythm IS the effect — anything synthetic feels fake.
- Render the chat in a Dialog or Sheet. It's a primary surface, not a popover.
- Show a "1m 23s" session timer. Reflection isn't billed by the second; the timer would change the experience.
- Style the assistant bubble with the accent color — bubbles compete with the coverage chips and bias the eye toward "AI is doing the work." Card-warm + soft border keeps the assistant present but quiet.
- Add `vaul` for the chat — see "Open / deferred" below.

## Summary (Phase 4)

A new top-level surface at `/summary`. Two sub-tabs, Radix `<Tabs>`:

- **Trends** (default) — `WeeklyLetter` is the hero, dashboard tiles fold below.
- **By Question** — vertical timeline per question, lifted from the deleted `HistoryByQuestion`.

The decision was *both* letter and dashboard, with the letter as the hero — see Open / deferred.

### Trends tab

```
<WeeklyLetter />                          ← PaperPage variant="letter", reads as a letter
<Card collapsible defaultOpen={false}>    ← "Stats this week"
  <MoodLine />
  <WordCloud />
  <StatTiles />                           ← streak · % answered · avg start time
  <GuruNote />                            ← optional, when extraction surfaced something
</Card>
```

#### `<WeeklyLetter>`

Wraps `<PaperPage variant="letter">`. Cadence: **a new letter arrives each Sunday morning** in the user's timezone. The backend already runs weekly `summary_jobs`; the prompt yields a `letter_md` field rendered verbatim into the paper page.

- **Framing** — opens with "Dear you," and signs off "— guru". Don't paraphrase or wrap with chrome.
- **Jump-to-day chips** — at the foot of the letter, a horizontally-scrolling row of 7 day-pills (Mon–Sun of the period). Each pill links to `/history/:date`, `bg-muted hover:bg-accent/15`.
- **Read aloud** — disabled `<Button>` with `<Badge>soon</Badge>` next to the title. Reuses the Phase 6b voice path when it lands.
- **Empty state** — when no letter has been generated yet (new user, mid-week), show `<PaperPage>` with copy "Your first letter arrives Sunday morning." in `body-prose`. No spinner; this isn't loading, it's pending.

#### Dashboard widgets

- `<MoodLine>` — small SVG chart, mood (1–5) over the last N days. Single `stroke-[hsl(var(--primary))]` line, dotted baseline at the user's median. No axes labels — the shape carries it. Reuse `MoodSparkline.tsx` if it fits.
- `<WordCloud>` — top N words from answers, sized by frequency. **One** word per cloud is `text-accent` — the LLM's "noticed" pick (carried in the summary payload). Other words use `text-foreground` at varying opacity by frequency. Lightweight client-only; no external lib.
- `<StatTiles>` — three tiles: current streak, % of active questions answered (last 7 days), avg start time. `font-mono tabular-nums` for numbers; `caption` eyebrow above each.
- `<GuruNote>` — see primitive below. Renders only when the summary payload includes a `noticed` narrative.

### By Question tab

Two-column layout (md+); single-column collapsible (sm).

- **Left rail** — list of active questions. Each row: kind chip + truncated prompt. Selected row gets `bg-accent/10 text-accent` and a 4px left margin-bar.
- **Right** — `<ByQuestionTimeline>` for the selected question. Vertical list, each item: `caption` date + answer body in `body-prose`, separated by a hairline `border-border/60`. Read-only. Tapping a date jumps to `/history/:date`.
- **Empty state** — "No answers yet for this question."

### `<GuruNote>` primitive

Accent-bordered narrative callout, reused on Summary dashboard and on `/history/:date` when an entry has a guru note.

```css
border-l-4 border-accent pl-4 py-2 italic text-foreground/90 font-serif
```

`<blockquote>` semantics. No icon; the margin-bar and italic serif carry the affect. Don't put `<GuruNote>` inside another `<Card>` with a border — the borders fight.

### Anti-patterns

Don't:
- Make the dashboard the default. The letter is the hero; dashboard is supporting.
- Add per-day pills above the WordCloud — temporal navigation lives in History.
- Show a spinner for the empty-state letter. Pending ≠ loading.
- Overlay a second Card border on `<GuruNote>`. The margin-bar IS the boundary.

## Settings

Tabbed surface at `/settings`. Four tabs, in order: **General · Questions · Notifications · Account**. Tab state is in the URL (`?tab=questions`) so deep links and reload preserve position; `motion.li layoutId` slides indicators on switch.

### General

- **Appearance** — palette picker (paper / ember / forest / ocean / slate) + light/dark mode toggle. Wireframes don't show this but it stays. Swatches are the only place where palette colors are hardcoded in JS (in `src/lib/palette.ts`).
- **Voice & Tone** — segmented chip `strict | warm | quiet`, default `warm`. Persisted to `users.guru_tone`. **Affects both chat AND summary letters** so the guru's voice is coherent across surfaces. The chosen tone is prepended as a `SystemCacheable` preamble on `CHAT_MODEL` and `SUMMARY_MODEL` so prompt-cache stays warm per-tone. Don't split this between General and Notifications — it's an identity setting, not a delivery setting.
- **Day start** — existing `day_start_minutes` slider (default 360 = 6am). Helper copy explaining late-night cutoff.
- **Timezone** — Radix Select with type-to-filter.

### Questions

The active question set drives both Manual flow (CardStack order) and By Question summary (rail order). Backed by `questions(user_id, position)` with a `DEFERRABLE INITIALLY DEFERRED` unique index so reorder transactions are atomic.

- **Row layout** — drag handle (`<GripVertical>`) · prompt (`h3`) · kind chip · enabled `<Switch>`. Drag handle uses `motion.li layoutId={q.id}` for slide-on-reorder.
- **Kind chip** — small `<Badge variant="secondary">`, one of: `short answer | sentence | sentence + chips | word | name + why`. Read-only on the row; tap-to-edit opens an inline `<Select>`.
- **Paused state** — when `active = false`, row goes `opacity-60` with a dashed `border-l-2 border-dashed border-muted-foreground/40`. Paused questions don't appear in CardStack and don't count toward streak level.
- **Add affordance** — bare `<Button variant="ghost">+ add question</Button>` at the bottom of the list. Opens an inline form (prompt + kind picker), not a Dialog.
- **Helper copy** — beneath the list, in `text-muted-foreground caption`: *"fewer is better — most people stick at 5–7."* Show a soft warning at >9 questions.
- **Reorder** — drag-and-drop on desktop; long-press-then-drag on touch. Optimistic; the deferred unique index lets the server accept the full reorder as one transaction.

### Notifications

- **Reminder time + days-of-week chips** — single row. The existing `<TimePicker>` on the left; a chip group `mon tue wed thu fri sat sun` on the right. Each chip toggles independently, `bg-accent/15 text-accent` when on, `bg-muted text-muted-foreground` when off. Backed by a new `users.reminder_dow` (text array or 7-bit integer; pick one in the migration). Default = all 7 days.
- **Per-device push card** — see Push reminders section. Subscribe button is per-device by design; iOS gating intercepts the entire card pre-A2HS.

### Account

Email (read-only), sign-out, delete-account `<AlertDialog>`.

### Anti-patterns

Don't:
- Put Voice & Tone under Notifications. It changes copy across chat and summary; placement under General communicates that scope.
- Use a browser `confirm()` for delete-account. All destructive confirms go through `<AlertDialog>`.
- Add a save button to Questions. Edits are optimistic; cache update is the feedback. Sticky save bar is reserved for tabs that batch changes (none currently).

## Push reminders (Phase 5)

- The reminders surface lives in **Settings → Notifications** alongside the existing reminder-time picker. There is no separate "Notifications" page; subscription is configuration, not navigation.
- Subscribe button is per-device by design — phones and laptops are separate `push_subscriptions` rows, and the "Send test notification" button always targets the device the user clicked from. The device list under the button shows other endpoints with the rough "Chrome on macOS · last seen 3d ago" label, so the user can audit and unsubscribe stale ones from this card.
- iOS gating is explicit: when we detect iPhone/iPad without `display-mode: standalone`, the entire card is replaced with the "Add to Home Screen first" copy. This isn't a hint at the bottom — it's the whole content, because the subscribe button physically cannot work, and offering it would leak frustration.
- The custom service worker (`src/sw/push-handler.ts`) handles `push`, `notificationclick`, and `pushsubscriptionchange`. The third one is the iOS-reliability linchpin — the SW silently re-subscribes against `/api/push/vapid-public-key` whenever the OS rotates the endpoint, so users don't lose reminders after a reboot.
- Notification visuals match the OS, not our palette — title `JournAI`, icon `/pwa-192.png`. No custom styling because the OS overrides everything anyway.

## Open / deferred

### Decisions made (2026-05)

- **Trends composition** — *both* WeeklyLetter and dashboard, with the letter as the hero. Dashboard folds below in a collapsible Card.
- **Editing past entries** — full inline edit on `<PaperPage>`. **No** amendments log. Manual-wins merge in the chat extraction worker is the safety net.
- **Q7 transition** — small celebratory micro-moment per Motion → Q7 morph. Never confetti.
- **Chats top-level** — renamed Today; **single chat session per `local_date`** (matches the `chat_sessions (user_id, local_date)` UNIQUE constraint). Past sessions surface only via `<HistoryChatTranscript>` on `/history/:date`. No inbox / sidebar of past chats.
- **Voice & Tone scope** — affects both chat system prompt and summary letters. Lives in General, not Notifications.

### Still deferred

- **Multi-session-per-day chats** — would require a schema change (drop the `local_date` UNIQUE, add an inbox surface). Revisit only if users request it.
- **Amendments log** for past entries — out of scope for v1; manual-wins merge handles the only realistic clobber path.
- **Read-aloud on the weekly letter** — disabled `<Badge>soon</Badge>` button. Will reuse the Phase 6b voice path (OpenAI Realtime ephemeral) when Talk lands.
- **`vaul` Sheet** — deferred; sticky surfaces work inline.
- **`cmdk` command palette** — deferred until power-user flows demand it.
- **Decorative paper-grain SVG** — skipped; cream + serif carry it.
- **React Compiler** — deferred until React 19 upgrade (compiler targets 19+).
