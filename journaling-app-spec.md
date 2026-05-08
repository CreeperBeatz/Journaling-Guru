# Journaling System Specification

## Goal

Help users run small experiments on their own life and know whether the experiments worked. The app supports a loop:

**observe daily → spot patterns weekly → commit to a change → measure it.**

The product is specifically an **energy/wellbeing pattern tracker**. It identifies what drains and what charges the user, and helps them act on it.

---

## Onboarding (one-time, ~2 min)

1. **What the goal of the App is**
2. **Weekly reflection day** — user picks the day of the week for the weekly review.
3. **Daily reminder time** — single notification at a chosen time of day.

---

## The Energy Audit template

Five fixed prompts per day. three are analyzed, two are not.

| # | Prompt | Type | Analyzed? |
|---|--------|------|-----------|
| 1 | Mood (sad / neutral / happy faces) | scale | yes |
| 2 | What drained you? | valenced tag (negative) | yes |
| 3 | What charged you? | valenced tag (positive) | yes |
| 4 | What are you grateful for? | gratitude, free text | no |
| 5 | Anything else on your mind? | reflective free text | no |

"Nothing today" is a valid answer for prompts 2-5. It doesn't generate a tag and doesn't count as a skipped question.

---

## Daily flow (~3 min)

The user opens the app and works through the prompts in order.

### Step 1 — Mood if the day

Three faces: sad, neutral, happy. One tap.

### Step 2 — Drained / Charged / Gratitude / Anything else

The user records what drained them, what charged them, what they're grateful for, and anything else on their mind.

> **LLM kicks in here.** Instead of four separate text fields, the LLM runs a single conversation that covers the four prompts as topics to weave in (not a strict checklist). If the user only wants to talk about one thing, that's fine. At the end, the LLM offers *"want to add anything about [uncovered topic]?"* — optional, never forced.
>
> **Without an LLM:** the user fills in four short text fields, in order, with the option to skip any.

Behind the scenes, the system extracts **tags** from the drainer and charger answers. A tag is a short label for a recurring thing (e.g. `social media`, `morning walk`, `back-to-back meetings`).

> **LLM kicks in here.** The LLM extracts tags from free-text answers and matches them against the user's existing tag list. If "scrolling Twitter for an hour" matches an existing `social media` tag, it reuses it. If it's new, it proposes a new tag.
>
> **Without an LLM:** the user can manually pick tags from a dropdown of their existing tags, with an "add new tag" option. Slower, but workable.

### Step 3 — Active goal check-ins

If the user has any active goals, they get a yes/no check-in for each at the end of the daily flow.

*"Did you stay under your screen-time limit today?"* → Yes / No.

No LLM needed. Just the daily question attached to each goal.

Have a section for comments if needed for the manual (non LLM) entry

### What the user does NOT see at the end of the day

No daily summary, no AI recap, no "here's what I noticed today." The user wrote the data; they don't need it paraphrased back. The summary lives at the weekly view and on the always-available summary page.

---

## Weekly reflection (~5 min, on the chosen day)

Triggered on the user's chosen weekly day, in place of (or after) the normal daily flow.

### Step 1 — Pattern view

The system shows:

- **Top drainers this week** — tag, appearance count, average mood on days that tag appeared
- **Top chargers this week** — same shape
- **Δ vs. prior week** — did this drainer/charger appear more or less than last week
- **Gratitude items from the week** — listed, not analyzed

No LLM needed for this step. It's a query against the tag table.

### Step 2 — Surprise prompt

*"Did anything surprise you this week?"* — free text, optional.

> **LLM kicks in here, optionally.** The LLM can lightly engage if the user writes something — *"Interesting that meetings showed up more than usual. Want to think about why?"* But it doesn't have to. A plain text field is fine.
>
> **Without an LLM:** plain text field, stored, never analyzed.

### Step 3 — Action prompt

*"Want to act on something?"* → opens goal creation if yes.

### Step 4 — Active goals progress

For each active goal, show kept-it count for the week (e.g. "5/7 days").

---

## Goal lifecycle

Goals are how the user closes the loop between *seeing a pattern* and *changing something*.

### Creation

A goal needs three things:

- **Title** — short, identifiable (e.g. "Cut phone use after 22:00")
- **Daily check-in question** — the yes/no the user answers each day
- **End date** — required, prevents runaway goals. Default 2 weeks.

> **LLM kicks in here.** The LLM helps shape the goal in conversation. If the user types something vague like *"stop doomscrolling,"* it proposes a measurable check-in question: *"want me to ask each evening whether you stayed under a screen-time limit, or just whether you doomscrolled today (yes/no)?"* The user SHOULD NOT be able to create a goal that isn't S.M.A.R.T. The LLM should help make it smart, if its not already.

Goals in the app should have a "time to keep them", iterable in weeks (up to next reflection). Minimal time is 1 week, maximum is indefinite.
>
> **Without an LLM:** the user fills in three fields manually. Title, check-in question, end date.



### Active phase

Each day, during the daily flow, the user gets the yes/no check-in for every active goal. That's it.

### End

On the end date, the system asks the user to wrap up the goal:

*"Your goal '[X]' ends today. How did it go?"* → kept the change / dropped it / inconclusive, plus optional why.

> **LLM kicks in here.** The LLM phrases the wrap-up prompt and lightly probes the "why" if the user is brief. *"You said 'dropped it' — what got in the way?"*
>
> **Without an LLM:** three buttons (kept / dropped / inconclusive) and an optional text field for the reason.

### Abandonment

The user can abandon a goal mid-stream with one tap. The system asks *"what didn't work?"* (optional). Failure data is more valuable than completion data — it tells the user what kinds of goals don't fit them.

---

## Summary page (always available)

Three zones, top to bottom. The user can scroll or tab between them.

### Zone 1 — At a glance

Readable in 5 seconds.

- Mood sparkline, last 30 days
- 7-day average vs. prior 7-day, with delta
- One-sentence headline insight (e.g. *"Your best days this week shared one thing: you exercised."*)
- Active goal status (e.g. "Day 9 of 14 — Cut coffee after 14:00")

> **LLM kicks in here, for the headline insight only.** The LLM looks at the week's tag table and writes a single sentence about the most informative pattern. If there's no clear pattern, it says so honestly.
>
> **Without an LLM:** a static headline like *"Top drainer this week: meetings (4 days, avg mood 2.1)"* generated from the tag table directly. Less narrative, equally useful.

For the first ~14 days, this zone shows a "still building your baseline" state instead of trends. Patterns need ~2 weeks to be meaningful.

### Zone 2 — What's driving it

Two tables:

**Drainers (last 30 days)**

| Tag | Appearances | Avg mood on those days |
|---|---|---|
| back-to-back meetings | 8 days | 2.4 |
| poor sleep | 6 days | 2.1 |

**Chargers (last 30 days)**

| Tag | Appearances | Avg mood on those days |
|---|---|---|
| deep work | 12 days | 4.2 |
| exercise | 9 days | 4.0 |

Tags with fewer than ~7 appearances get a faint "low confidence" marker. Not hidden — just flagged.

No LLM needed. Direct query against the tag table.

### Zone 3 — What I tried

A list of all goals, active and historical:

- Title
- Outcome (kept / dropped / inconclusive)
- One-line conclusion
- Date range

This is the artifact that justifies the whole exercise. It's evidence the user's life has been investigated, not just recorded.

No LLM needed. Direct query against the goals table.

---

## Data model (sketch)

```
DailyEntry
  date
  mood: 1-3
  drained_text, drained_tags[]
  charged_text, charged_tags[]
  gratitude_text
  reflection_text
  created_at, edited_at, backfilled: bool

Tag
  id, label
  valence: positive | negative | neutral
  status: active | merged | archived

Goal
  id, title
  check_in_question
  start_date, end_date
  status: active | completed | abandoned
  conclusion_outcome: kept | dropped | inconclusive | null
  conclusion_text

GoalCheckIn
  goal_id, date, value: yes | no

Question (for future template expansion)
  id (stable), text (mutable)
  type: valenced_tag | state_observation | reflective_text | gratitude | scale
  valence_polarity: positive | negative | neutral | null
  status: active | archived
```

Notes:

- Tag IDs are permanent. Renaming a tag updates the label, not the ID. History is preserved.
- Removing a question archives it, never deletes. Old entries keep their data.
- Backfill allowed up to 2-3 days. Backfilled entries are flagged so they can be analyzed separately if needed.
- Edits allowed indefinitely. Edited entries get an `edited_at` timestamp.

---

## Constraints
- **No social/sharing features.** Different product.
- **No daily AI summary.** Tempting, premature, adds noise.