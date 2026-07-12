---
name: Praetor
description: The control plane for a resilient, Kubernetes-native Ansible automation platform.
colors:
  brand-accent: "#4763e6"
  brand-action: "#3049c9"
  brand-action-hover: "#2739a3"
  brand-action-active: "#243284"
  brand-tint: "#eef2ff"
  surface-bg: "#f4f5f8"
  surface: "#ffffff"
  ink: "#1e2230"
  ink-heading: "#111827"
  ink-muted: "#6b7280"
  border: "#e5e7eb"
  border-strong: "#d1d5db"
  sidebar-deep: "#111524"
  success: "#047857"
  success-surface: "#ecfdf5"
  warning: "#b45309"
  warning-surface: "#fffbeb"
  error: "#dc2626"
  error-surface: "#fef2f2"
  dag-signal: "#0d9488"
  dag-signal-text: "#0f766e"
  dag-signal-surface: "#f0fdfa"
  dag-dispatch: "#7c3aed"
  dag-dispatch-text: "#6d28d9"
  dag-dispatch-surface: "#f5f3ff"
typography:
  headline:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "-0.02em"
  title:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "-0.01em"
  body:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
  label:
    fontFamily: "Geist Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.625rem"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "0.14em"
  data:
    fontFamily: "Geist Mono Variable, ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
    fontVariation: "tabular-nums"
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
  xl: "12px"
spacing:
  sm: "8px"
  md: "16px"
  lg: "24px"
components:
  button-primary:
    backgroundColor: "{colors.brand-action}"
    textColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    padding: "8px 16px"
  button-primary-hover:
    backgroundColor: "{colors.brand-action-hover}"
    textColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    padding: "8px 16px"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "#374151"
    rounded: "{rounded.lg}"
    padding: "8px 16px"
  button-danger:
    backgroundColor: "{colors.error}"
    textColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    padding: "8px 16px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.lg}"
    padding: "8px 16px"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink-heading}"
    rounded: "{rounded.lg}"
    padding: "8px 12px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
    padding: "24px"
  badge-info:
    backgroundColor: "{colors.brand-tint}"
    textColor: "#2739a3"
    rounded: "{rounded.md}"
    padding: "2px 8px"
---

# Design System: Praetor

## 1. Overview

**Creative North Star: "The Control Plane"**

Praetor is the authoritative deck from which fleets are run. The interface reads like a command surface for serious infrastructure work: a deep cobalt identity, dense-but-composed layouts, and honest status everywhere. It is built for a team that spans the calm configuring hour and the tense incident minute in a single sitting — platform engineers wiring up templates and inventories, playbook authors buried in per-host logs, on-call responders confirming what actually converged after an outage. Every surface should reinforce the platform's central promise: the state shown is the real, converged truth of what happened on the executors, never a hopeful projection.

The system is deliberately quiet. Character comes from restraint and craft, not decoration — the single cobalt accent, tabular figures that align in columns, a whisper-tint dot-grid on the body surface, and cool-tinted shadows that separate content without drama. Density is a feature: information-rich tables, log viewers, and panels are welcome where the work demands them, but density must read as command, not clutter.

This system explicitly rejects four things. It is **not** the cramped, enterprise-Java feel of legacy AWX / Ansible Tower — the thing being replaced. It is **not** a generic AI-SaaS template: no purple gradients, hero-metric card walls, decorative glassmorphism, or endless identical icon-card grids. It is **not** consumer or playful — no mascots, candy colors, or marketing flourish. And it is **not** sterile enterprise gray — no flat clinical white, dead grays, or zero-warmth surfaces.

**Key Characteristics:**
- Single authoritative accent: one deep cobalt ("Prussian"), used for action, selection, and state — never decoration.
- Cool-tinted, textured light surface (`#f4f5f8` with a faint dot-grid) — never flat clinical white.
- One type family (Geist) in weights, with Geist Mono for data and logs; tabular figures wherever numbers matter.
- Dense but legible: compact controls, calm status vocabulary, structural depth that stays quiet at rest.
- Honest state above all — status is never ambiguous, and color is never the only signal.

## 2. Colors

A cool, near-monochrome neutral field carrying a single deep-cobalt accent, with a restrained semantic set reserved for job and system state.

### Primary
- **Prussian Cobalt** (`#3049c9`, `brand-action`): The action color. Primary buttons, the app logo mark, and the active-selection accent. Its 500-step sibling **Cobalt Link** (`#4763e6`, `brand-accent`) carries links, focus rings, and info accents. Deeper steps — **Cobalt Pressed** (`#2739a3`) and **Cobalt Deep** (`#243284`) — are hover and active states only.
- **Cobalt Tint** (`#eef2ff`, `brand-tint`): The wash behind info badges and selected rows. The only place the brand hue appears as a fill rather than an accent.

### Neutral
- **Command Ink** (`#1e2230`): Default body text on light surfaces. A cool near-black, never pure `#000`.
- **Heading Ink** (`#111827`): Headings, primary values, high-emphasis labels.
- **Muted Ink** (`#6b7280`): Secondary text, hints, and inactive labels. Must clear 4.5:1 on white — do not push muted text lighter than this for "elegance."
- **Control-Plane Surface** (`#f4f5f8`): The body background. A cool off-white overlaid with a faint dot-grid (`radial-gradient(rgb(24 27 40 / 0.028) 1px, transparent 1px)`, 22px). This texture is the identity — never replace it with flat `#fff` or `#f8fafc`.
- **Panel White** (`#ffffff`): Cards, tables, inputs, modals — the raised working surfaces that sit above the tinted body.
- **Deck Deep** (`#111524`): The sidebar. A near-black cobalt-tinted gradient (`gray-900 → #111524`) that anchors navigation as the dark command rail against the light content.
- **Hairline** (`#e5e7eb`) and **Hairline Strong** (`#d1d5db`): Card borders and control strokes.

### Named Rules
**The One Accent Rule.** Praetor has exactly one brand hue. Cobalt marks action, current selection, focus, and info — nothing else. If cobalt is decorating a surface rather than signaling something actionable, remove it. Its authority comes from scarcity.

**The Honest Surface Rule.** The body is `#f4f5f8` with its dot-grid, always. Flat clinical white (`#fff`, `#f8fafc`) is forbidden as a full-page background — it is the sterile-enterprise look the platform rejects.

### Semantic (job & system state)
- **Success** (`#047857` text on `#ecfdf5`): Converged / passed runs.
- **Warning** (`#b45309` text on `#fffbeb`): Degraded, queued-too-long, needs attention.
- **Error** (`#dc2626` text on `#fef2f2`): Failed runs and destructive actions.
- **Info** (`#2739a3` text on `#eef2ff`): Running / neutral system state.

**The State-Never-By-Color-Alone Rule.** Job status is always carried by a label and, where live, a status dot — never hue alone. Pass, fail, and running must be distinguishable without color.

### Workflow Status Palette (data-viz extension)
The workflow graph is categorical data visualization — the one surface that legitimately needs more than the four semantic hues to keep node types and run states apart at a glance. It reuses the semantic set where it maps cleanly (successful → success, failed → error, running → cobalt, awaiting-approval → warning) and adds two sanctioned categorical hues, both cool and desaturated to sit inside the system rather than shout:
- **Signal Teal** (`#0d9488` stroke, `#0f766e` text, `#f0fdfa` surface): inbound / event states — `awaiting_event` nodes and inbound webhook (`webhook_in`) nodes. The colour of "waiting on something to arrive."
- **Dispatch Violet** (`#7c3aed` stroke, `#6d28d9` text, `#f5f3ff` surface): outbound webhook (`webhook_out`) nodes — the colour of "calling out."

**The Data-Viz Exception Rule.** Signal Teal and Dispatch Violet exist for the workflow DAG and nothing else. They are forbidden in general chrome — buttons, nav, badges, links, backgrounds. Their whole justification is categorical legibility inside the graph; the moment they appear elsewhere, the single-cobalt-accent identity breaks. One accent everywhere; a scoped categorical palette only inside the data visualization.

## 3. Typography

**Display / Body Font:** Geist Variable (with `ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif`)
**Data / Mono Font:** Geist Mono Variable (with `ui-monospace, SFMono-Regular, Menlo, Monaco, monospace`)

Both are self-hosted and bundled at build time — no CDN, air-gap safe. There is no separate display face: Geist carries headings, buttons, labels, and body through weight alone, and Geist Mono carries everything numeric or log-shaped.

**Character:** Geist gives the UI quiet character without shouting — a technical humanist sans that stays crisp at small sizes. The sans/mono split is the only pairing, and it is functional: prose and controls in Geist, data and logs in Geist Mono, so the eye reads structure from typeface.

### Hierarchy
- **Headline** (600, 1.5rem, tracking `-0.02em`): Page titles. One per screen.
- **Title** (600, 0.9375rem/15px, tracking `-0.01em`): Card and section headers, modal titles. Small and tight, not loud.
- **Body** (400, 0.875rem/14px, line-height 1.5): The workhorse size for nearly all UI text and controls. Prose blocks cap at 65–75ch.
- **Label** (600, 0.625rem/10px, tracking `0.14em`, uppercase): Sidebar section dividers and micro-labels only. Field labels use 14px medium, not this.
- **Data** (Geist Mono, 400, ~0.8125rem, tabular-nums): Table values, metrics, IDs, timestamps, and log output. Digits align in columns.

### Named Rules
**The Tabular Digit Rule.** Everywhere numbers matter — tables, metrics, logs — digits use `tabular-nums` so columns of numbers align instead of dancing. This is enforced globally on `table` and `[data-nums]`.

**The Fixed-Scale Rule.** Type sizes are a fixed rem scale, never fluid `clamp()`. A heading that shrinks inside a sidebar looks worse, not designed. Product density wants predictable sizes at consistent DPI.

## 4. Elevation

Depth is **structural and quiet by default**. Raised working surfaces — cards, tables, modals, popovers — sit on soft, cool-tinted shadows that lift them off the textured body surface; interaction adds a small, deliberate response. Shadows are tinted to the cool neutral hue (`rgb(24 27 40)`), never pure black, so elevation feels soft rather than sterile. The dark sidebar and the light content are separated by tone and a hairline, not by heavy shadow.

### Shadow Vocabulary
- **Hairline lift** (`box-shadow: 0 1px 2px 0 rgb(24 27 40 / 0.06)` — `xs`): Inputs and secondary buttons at rest.
- **Resting card** (`0 1px 2px 0 rgb(24 27 40 / 0.06), 0 1px 3px 0 rgb(24 27 40 / 0.08)` — `sm`): The default card and panel elevation.
- **Raised** (`0 4px 8px -2px rgb(24 27 40 / 0.08), 0 8px 16px -4px rgb(24 27 40 / 0.08)` — `md`): Hover state for interactive cards; dropdowns.
- **Overlay** (`0 24px 48px -12px rgb(24 27 40 / 0.18)` — `xl`): Modals and dialogs, above a `backdrop-blur-sm` scrim.
- **Accent glow** (`0 6px 16px -4px rgb(48 73 201 / 0.35)` — `brand`): The one cobalt-tinted shadow, reserved for the primary button on hover. Used sparingly, as emphasis.

### Named Rules
**The Cool-Shadow Rule.** Every shadow is tinted to `rgb(24 27 40)`, never black. Pure-black shadows read sterile and hard; the cool tint is what keeps a dense UI from feeling clinical.

**The Lift-On-Intent Rule.** Surfaces are calm at rest. A card only lifts (`hover:-translate-y-0.5` + raised shadow) when it is genuinely clickable; a button presses (`active:translate-y-px`) on click. Motion is a response to intent, never ambient.

## 5. Components

Components should feel **tactile and responsive** — crisp edges, quick 150ms transitions, and a subtle physical press — confident tooling that reacts exactly to input without bouncing or flourishing. Every interactive component ships all of its states: default, hover, focus-visible, active, disabled.

### Buttons
- **Shape:** Softly rounded (8px, `rounded-lg`). Consistent across all variants.
- **Primary:** Cobalt fill (`brand-action` / `#3049c9`), white text, resting `sm` shadow. Hover deepens to `#2739a3` and adds the cobalt accent glow; active presses to `#243284` and translates down 1px. Padding `8px 16px` at the default `md` size (`sm`: `6px 12px`, `lg`: `10px 24px`).
- **Secondary:** White fill, `#374151` text, hairline border (`#d1d5db`), `xs` shadow. Hover warms the fill to `gray-50` and strengthens the border.
- **Danger:** Red fill (`#dc2626`), white text, for destructive actions only.
- **Ghost:** Transparent, muted text; hover fills `gray-100`. For low-emphasis and toolbar actions.
- **Focus:** A 2px cobalt ring at `brand-accent/60` with a 2px white offset. Never removed, only restyled. The app-wide keyboard-focus outline (`:focus-visible`) uses the tightest 4px (`sm`) radius so the ring hugs the focused element.

### Badges
- **Style:** Ring-based tokens, not solid pill fills — a tinted surface (`success-surface` etc.), colored text, and a 1px inset ring at ~20% opacity. Reads as calm "system" state rather than a loud sticker.
- **Variants:** `success` / `warning` / `error` / `info` / `neutral`, each on its own tint. Rounded 6px (`md`), 10–12px text.
- **Dot:** An optional leading status dot, used **only** when the badge conveys live state — not as decoration.

### Cards / Containers
- **Corner Style:** 12px (`rounded-xl`).
- **Background:** Panel White on the tinted body.
- **Shadow Strategy:** Resting `sm` shadow (see Elevation). `hoverable` cards add a `md` shadow and a 0.5px upward translate — used only when the whole card is a link.
- **Border:** 1px hairline (`gray-200/80`), with an optional header divider (`gray-100`).
- **Internal Padding:** 24px (`lg`) default; removable for edge-to-edge tables.
- **Never nest cards.** A card inside a card is always wrong; use a divider, a table, or a bare region instead.

### Inputs / Fields
- **Style:** White fill, 1px `gray-300` stroke, 8px radius, `xs` shadow, 14px text. Labels are 14px medium, bound to their control; hints are 12px muted, errors 12px red.
- **Focus:** Border shifts to cobalt (`brand-accent`) with a soft 2px `brand-accent/30` ring — a glow, not a hard outline.
- **Error:** Red border (`#f87171`) plus a 12px red message; `aria-invalid` set. **Disabled:** `gray-50` fill, muted text, not-allowed cursor.

### Navigation (Sidebar)
- **Style:** A fixed 256px dark rail — a `gray-900 → #111524` vertical gradient with a `white/5` right border. Grouped sections (Resources, Access) under 10px uppercase tracked labels.
- **States:** Idle items are `gray-400`; hover lifts to `gray-100` on a `white/4` wash; the active route gets a `white/6` fill, white text, a cobalt icon, and a 2px cobalt left marker. Transitions are color-only, 150ms.
- **Mobile:** Off-canvas drawer that slides in (`-translate-x-full` → `translate-x-0`); pinned at `lg+`.

### Modal
- **Backdrop:** `gray-900/50` with `backdrop-blur-sm` — the one sanctioned decorative blur, and only here.
- **Panel:** Panel White, 12px radius, `xl` overlay shadow, a `gray-900/5` ring. Focus is trapped and restored to the trigger on close; Escape closes.
- **Restraint:** A modal is the last resort, not the first. Exhaust inline and progressive alternatives before reaching for one.

### Workflow DAG (signature component)

Praetor's most distinctive surface: a hand-rolled, dependency-free SVG that renders a workflow as a left-to-right layered directed graph. One component (`WorkflowDag`) serves three contexts — the builder, the template detail view, and the live run view — with no external graph library. This is where "truth over optimism" is most visible: in the run view, node fills are driven by real per-node status, so the graph *is* the converged state of the workflow.

- **Layout:** Columns are assigned by longest-path depth from a root; nodes stack within a column. Node box is 168×52px with 8px corners; columns gap 76px, rows gap 28px, 16px canvas margin. The canvas scrolls inside a `gray-50` panel with a 1px `gray-200` border and 6px (`md`) corners.
- **Nodes:** A rounded rect (1.5px stroke) with a 13px/600 title (truncated past ~20 chars) and an 11px sub-line at 85% opacity. A leading glyph marks node type.
- **Edges:** Orthogonal elbow paths with rounded corners and a filled arrowhead, routed parent-right-center → child-left-center, 2px stroke. **Edge color encodes branch semantics:** success `#16a34a`, failure `#dc2626`, always `#6b7280`.
- **Run-view node status (fill / stroke / text):** successful & approved `#ecfdf5`/`#059669`/`#047857` (success); failed, error, lost & rejected `#fef2f2`/`#dc2626`/`#b91c1c` (error); running `#eef2ff`/`#4763e6`/`#2739a3` (cobalt — active on brand); awaiting approval `#fffbeb`/`#d97706`/`#b45309` (warning); awaiting event `#f0fdfa`/`#0d9488`/`#0f766e` (Signal Teal); skipped `#f9fafb`/`#d1d5db`/`#6b7280` (neutral, muted); pending `#ffffff`/`#d1d5db`/`#6b7280` (neutral, lightest).
- **Builder/detail node tone (by type, when there is no live status):** job `#eef2ff`/`#4763e6` (cobalt tint — the common node, tied to the brand); approval amber (warning — it pauses for a human); webhook-in Signal Teal (`#0d9488`); webhook-out Dispatch Violet (`#7c3aed`).
- **Icons (Lucide, nested SVGs, colored by node tone):** job → Play, approval → Pause, webhook-in → ArrowDownToLine, webhook-out → ArrowUpFromLine.
- **Empty state:** a centered 14px muted-italic "No nodes to display." — not a blank canvas.

**The Graph-Is-Truth Rule.** In the run view the DAG's colors are never decorative — every fill reflects an actual node status. If a node is emerald it converged; if it is cobalt it is running now. The graph must always agree with the underlying run, never lag or guess.

All node and edge colors resolve to design-system tokens: the four semantic hues plus the scoped Signal Teal / Dispatch Violet from the Workflow Status Palette (see §2). No bespoke greens, blues, or purples remain, and node type is now carried by a Lucide glyph rather than an emoji — so a "successful" node reads identically here and in a Badge.

## 6. Do's and Don'ts

### Do:
- **Do** keep cobalt scarce — action, selection, focus, and info only. One accent, earning its authority through rarity (**The One Accent Rule**).
- **Do** keep the body on `#f4f5f8` with its dot-grid, and keep working surfaces Panel White above it.
- **Do** use Geist Mono with `tabular-nums` for all data, IDs, timestamps, and logs so numeric columns align.
- **Do** carry job status with a label (and live dot) as well as color — status must survive without hue.
- **Do** tint every shadow to `rgb(24 27 40)` and let surfaces stay calm at rest, lifting only on genuine intent.
- **Do** ship every interactive component with default, hover, focus-visible, active, and disabled states.
- **Do** honor `prefers-reduced-motion` — the global reset is already in place; keep transitions state-driven and ≤250ms.

### Don't:
- **Don't** reproduce the cramped, dated **AWX / Ansible Tower** look — the enterprise-Java feel of the thing being replaced.
- **Don't** drift toward the **generic AI-SaaS template**: no purple gradients, hero-metric card walls, decorative glassmorphism, or endless identical icon-card grids.
- **Don't** go **consumer or playful** — no mascots, candy colors, big illustrations, or marketing flourish. This is expert infrastructure tooling.
- **Don't** ship **sterile enterprise gray** — no flat clinical `#fff`/`#f8fafc` page backgrounds, no dead grays, no zero-warmth surfaces.
- **Don't** use `background-clip: text` gradient headings, or a colored `border-left`/`border-right` stripe as an accent on cards, list items, or alerts.
- **Don't** nest a card inside a card, or reach for a modal before exhausting inline alternatives.
- **Don't** use fluid `clamp()` type or a second display typeface — one Geist family, fixed rem scale.
- **Don't** let muted text drop below 4.5:1; light gray "for elegance" is forbidden.
