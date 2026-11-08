# Okabe Night: Dark-Only, Colorblind-Safe Color System

A dark-mode color system for web apps, desktop GUIs and mobile apps. The hues come from the **Okabe-Ito / Color Universal Design (CUD)** palette, rebuilt as perceptual (OKLCH) tonal ramps so every color has a safe shade for text, fills, borders and tinted backgrounds on a dark canvas. Data viz palettes come from **Okabe-Ito, Viridis/Cividis, Paul Tol and ColorBrewer**.

Every text and UI pair in this file has been checked against WCAG 2.2 contrast, and status colors have been checked under simulated protanopia, deuteranopia and tritanopia.

**Files that come with this spec:**
- `tokens.css` - CSS custom properties (primitive ramps + semantic tokens + viz)
- `tailwind-theme.css` - Tailwind CSS v4 `@theme` block (replaces Tailwind's default palette)
- `tokens.json` - platform-neutral design tokens (DTCG format) to convert for SwiftUI, Compose, Flutter, React Native, Qt, etc.

---

## 1. Rules for Claude Code (follow these on every project)

1. **Dark mode only.** Set `color-scheme: dark` (web), force dark appearance on iOS/Android/desktop. Never generate a light theme, a theme toggle, or `prefers-color-scheme: light` styles unless explicitly asked.
2. **Components use semantic tokens only** (`bg.surface`, `text.primary`, `danger.solid`...). Never hard-code hex values or primitive ramp steps in components. If a needed role is missing, add a semantic token rather than reaching for a raw color.
3. **Never use color as the only signal.** This is the core CUD rule.
   - Status messages, badges and toasts always pair color with an **icon and a text label** (see section 3).
   - Form errors show an icon + message text, not just a red border.
   - Links inside body text are underlined. Standalone nav links can skip the underline.
   - Charts use **markers, line dashes or direct labels** in addition to color (see section 5). Prefer direct labels on the chart over a separate legend.
4. **Contrast minimums (WCAG 2.2 AA):** 4.5:1 for normal text, 3:1 for large text (24px+, or 18.66px+ bold), 3:1 for UI component boundaries and meaningful graphics. All text tokens below already meet 4.5:1 on every surface.
5. **No red/green pairings.** Danger is **vermilion** (orange-red) and success is **bluish green**. These stay distinguishable for red-green colorblind users where pure red and pure green do not. Never use `#FF0000`, `#00FF00` or Tailwind's default red/green.
6. **No pure black, no pure white.** Canvas is `#090E13` and primary text is `#EDF0F4`. Pure white on pure black causes halation (glow/blur) for many readers, especially those with astigmatism.
7. **Elevation goes lighter, not darker.** In dark UIs higher layers get lighter surfaces: canvas → surface → raised → overlay. Shadows still help but surface lightness carries the hierarchy.
8. **Avoid low-saturation, low-contrast color combinations** (a CUD "don't"). Use tinted `*.subtle` backgrounds only with their matching `*.text` color on top.
9. **Adjacent categories alternate warm and cool** where possible (orange next to blue, vermilion next to sky blue). The categorical order in section 5 already does this.
10. **Name colors in UI copy only alongside another cue.** Never write "click the green button"; write "click **Save**".

---

## 2. Semantic tokens

### Surfaces and borders
| Token | Hex | Use |
|---|---|---|
| `bg.canvas` | `#090E13` | App/page background (lowest layer) |
| `bg.surface` | `#0F1319` | Cards, panels, sidebars, list rows |
| `bg.raised` | `#151A20` | Popovers, dropdowns, elevated cards |
| `bg.overlay` | `#1C2127` | Modals, dialogs, tooltips |
| `bg.sunken` | `#090E13` | Inset wells inside a surface (code blocks, input fill) |
| `bg.hover` | `#23282F` | Hover state for rows/menu items |
| `bg.active` | `#2D3239` | Pressed state for rows/menu items |
| `bg.selected` | `#00253F` | Selected row/tab background |
| `border.subtle` | `#23282F` | Dividers inside a card |
| `border.default` | `#2D3239` | Card and panel outlines |
| `border.strong` | `#65696F` | Input, checkbox and toggle outlines (meets 3:1) |

### Text
| Token | Hex | Use |
|---|---|---|
| `text.primary` | `#EDF0F4` | Body copy, headings |
| `text.secondary` | `#B0B5BB` | Supporting copy, labels |
| `text.muted` | `#94999E` | Captions, timestamps, placeholders |
| `text.disabled` | `#4C5259` | Disabled controls only (exempt from contrast rules) |
| `text.inverse` | `#090E13` | Text on light fills |
| `text.link` | `#82CEFC` | Links (underline them in body text) |
| `text.link-hover` | `#ADDFFF` | Link hover |

Contrast on the darkest-to-lightest surface: text.primary ≥ 14:1, text.secondary ≥ 7.8:1, text.muted ≥ 5.6:1.

### Brand: primary (blue) and accent (orange)
Primary is the Okabe-Ito blue family, which stays distinct under every type of color vision deficiency. Accent is Okabe-Ito orange, its warm complement.

| Token | Hex | Use |
|---|---|---|
| `primary.solid` | `#5CBAF3` | Primary buttons, active toggles, progress |
| `primary.hover` | `#82CEFC` | Primary hover |
| `primary.pressed` | `#35A2E3` | Primary pressed |
| `primary.on` | `#090E13` | Text/icons on primary.solid |
| `primary.subtle` | `#00253F` | Tinted bg for primary chips/selected states |
| `primary.text` | `#82CEFC` | Primary-colored text on dark surfaces |
| `primary.border` | `#0B6EAB` | Outline for primary subtle elements |
| `accent.solid` | `#E7A000` | Highlights, CTAs that must pop, badges |
| `accent.hover` | `#FCB538` | Accent hover |
| `accent.pressed` | `#CC8800` | Accent pressed |
| `accent.on` | `#090E13` | Text/icons on accent.solid |
| `accent.subtle` | `#371A00` | Tinted accent background |
| `accent.text` | `#FCB538` | Accent text |
| `accent.border` | `#935A00` | Accent outline |
| `focus.ring` | `#E7A000` | Keyboard focus outline (2px, 2px offset) |
| `highlight.solid` | `#E88EC0` | Tertiary / 'new' / premium tags |
| `highlight.on` | `#090E13` | Text on highlight.solid |
| `highlight.subtle` | `#381329` | Tertiary tint |
| `highlight.text` | `#F7A8D3` | Tertiary text |
| `highlight.border` | `#944E76` | Tertiary outline |

**Destructive buttons** default to the outlined style (`danger.subtle` bg, `danger.text` label, `danger.border`, plus an icon) and only turn `danger.solid` on hover or inside a confirmation dialog. Never place a solid `accent` button right next to a solid `danger` button: orange and vermilion are only moderately distinct for deuteranopes (ΔE 7.1), so the icon and outline carry the difference.

**Buttons on dark use dark text on a light fill** (`primary.on` on `primary.solid` is about 9:1). This reads better than white text on a mid-blue and keeps the button luminous. Use `accent` sparingly: one accent element per view is a good default.

### Utility
| Token | Value | Use |
|---|---|---|
| `selection.bg` | `#005485` | Text selection (text.primary on it = 7.0:1) |
| `effect.scrim` | `rgba(9, 14, 19, 0.72)` | Backdrop behind modals |
| `effect.shadow` | `rgba(0, 0, 0, 0.55)` | Drop shadows (dark UIs need strong, soft shadows) |
| `chart.grid` | `#23282F` | Chart gridlines |
| `chart.axis` | `#4C5259` | Chart axis lines and ticks |
| `chart.label` | `#94999E` | Axis labels, legends |
| `chart.tooltip-bg` | `#1C2127` | Chart tooltip background |

---

## 3. Status colors

| Role | Hue | `solid` | `text` | `subtle` (bg) | `border` | Required icon |
|---|---|---|---|---|---|---|
| **success** | Bluish green (Okabe-Ito) | `#1CB283` | `#80DAB4` | `#002B1C` | `#007C59` | `check-circle` |
| **warning** | Yellow (Okabe-Ito) | `#E9DD31` | `#E9DD31` | `#301F00` | `#816500` | `alert-triangle` |
| **danger** | Vermilion (Okabe-Ito) | `#EC6E1F` | `#FFAE86` | `#411000` | `#AB4500` | `x-octagon / alert-octagon` |
| **info** | Blue (Okabe-Ito sky blue) | `#5CBAF3` | `#82CEFC` | `#00253F` | `#0B6EAB` | `info-circle` |

Each role also has `*.hover` and `*.on` (text on solid, always `#090E13`).

**Patterns:**
- **Inline alert / banner:** `*.subtle` background, 1px `*.border`, icon + title in `*.text`, body in `text.primary`.
- **Badge / pill:** `*.subtle` bg + `*.text` label, or `*.solid` bg + `*.on` label for high emphasis. Always include an icon or a word ("Failed", "Live").
- **Toast:** `bg.overlay` with a 3px left bar in `*.solid` plus icon.
- **Form error:** border switches to `danger.solid`, icon + message below in `danger.text`, and `aria-invalid="true"`.
- **Status dot** (e.g. online/offline): never alone. Pair with text, or use different shapes (filled circle = online, hollow ring = away, square = offline).

**Color-blind separation of status fills** (CIEDE2000 ΔE; above ~10 is clearly distinct, above 20 is very distinct; simulated with Machado 2009 for protan/deutan and Brettel 1997 for tritan):

| Pair | Normal | Protan | Deutan | Tritan |
|---|---|---|---|---|
| success vs warning | 34.5 | 23.1 | 28.7 | 45.7 |
| success vs danger | 56.0 | 17.9 | 20.3 | 57.8 |
| success vs info | 35.1 | 36.4 | 32.1 | 6.3 |
| success vs highlight | 66.0 | 29.2 | 17.2 | 49.5 |
| warning vs danger | 40.7 | 25.2 | 16.6 | 25.2 |
| warning vs info | 58.5 | 53.9 | 56.6 | 45.2 |
| warning vs highlight | 61.9 | 47.6 | 37.9 | 14.8 |
| danger vs info | 51.4 | 51.8 | 53.3 | 61.4 |
| danger vs highlight | 36.9 | 42.6 | 35.0 | 11.5 |
| info vs highlight | 45.3 | 9.6 | 15.4 | 51.6 |

The lowest value is success vs info for tritanopes (very rare, about 0.01% of people). The required icons cover that case.

---

## 4. Primitive ramps

Ramps are built in OKLCH with consistent lightness per step across hues, so `blue-300` and `green-300` have about the same contrast. Hue drifts slightly along some ramps to match Okabe-Ito (blue goes from sky blue to deep blue) and to keep dark yellows/oranges amber instead of olive or muddy brown.

| Hue | 50 | 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 | 950 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| blue | `#EEF7FC` | `#D4EEFF` | `#ADDFFF` | `#82CEFC` | `#5CBAF3` | `#35A2E3` | `#0A88CC` | `#0B6EAB` | `#005485` | `#00385C` | `#00253F` |
| green | `#EEF8F3` | `#D3F2E3` | `#AAE8CB` | `#80DAB4` | `#54C89C` | `#1CB283` | `#00976D` | `#007C59` | `#005F43` | `#00402C` | `#002B1C` |
| yellow | `#FAF8D1` | `#F7EF80` | `#E9DD31` | `#D5C923` | `#C5B102` | `#B19800` | `#997E00` | `#816500` | `#654B00` | `#473100` | `#301F00` |
| orange | `#FEF4E5` | `#FFE5C1` | `#FFCF86` | `#FCB538` | `#E7A000` | `#CC8800` | `#B07000` | `#935A00` | `#734200` | `#512B00` | `#371A00` |
| vermilion | `#FFF2EC` | `#FFE3D6` | `#FFCAB1` | `#FFAE86` | `#FF8C4F` | `#EC6E1F` | `#CC5900` | `#AB4500` | `#863100` | `#5E1D00` | `#411000` |
| purple | `#FDF2F7` | `#FFDFEF` | `#FFC3E2` | `#F7A8D3` | `#E88EC0` | `#D175A9` | `#B2618F` | `#944E76` | `#73395A` | `#51233E` | `#381329` |

**Neutral (cool blue-gray, hue 255):** 0 `#F7FAFE` · 50 `#EDF0F4` · 100 `#E0E3E7` · 200 `#CDD0D3` · 300 `#B0B5BB` · 400 `#94999E` · 500 `#7C8186` · 600 `#65696F` · 700 `#4C5259` · 800 `#383E45` · 850 `#2D3239` · 900 `#23282F` · 925 `#1C2127` · 950 `#151A20` · 975 `#0F1319` · 1000 `#090E13`

On dark backgrounds: steps **200-400** are for text and solid fills, **600-700** for borders and lines, **900-950** for tinted backgrounds. Yellow 300-500 trends olive, so prefer yellow 100-200.

---

## 5. Data visualization

### Categorical (default for charts)
Okabe-Ito in the recommended order for 2, 3, 4... categories. Two changes for dark mode: Okabe-Ito **black is replaced with light gray**, and Okabe-Ito blue is lifted slightly (`#0072B2` → `#0A88CC`) so it clears 3:1 against dark surfaces. Yellow stays last.

| # | Name | Hex | Marker | Line style | vs bg.surface |
|---|---|---|---|---|---|
| 1 | orange | `#E69F00` | circle | solid | 8.3:1 |
| 2 | blue-lifted | `#0A88CC` | square | dashed (6 4) | 4.8:1 |
| 3 | bluish-green | `#009E73` | triangle | dotted (2 3) | 5.4:1 |
| 4 | vermilion | `#D55E00` | diamond | dash-dot | 4.8:1 |
| 5 | sky-blue | `#56B4E9` | cross (+) | long dash (10 4) | 8.1:1 |
| 6 | reddish-purple | `#CC79A7` | x | solid, thick | 6.1:1 |
| 7 | light-gray | `#CDD0D3` | star | dashed, thin | 12.0:1 |
| 8 | yellow | `#F0E442` | inverted triangle | dotted, thick | 14.1:1 |

- For **2 series** use 1-2 (orange + blue). For **3** add 3, and so on.
- **More than 8 categories?** Don't add colors. Group into "Other", use small multiples, or label directly.
- Keep the **same category → color mapping** across every chart in the app.
- **Lines:** 2px minimum. Thin lines and small markers should favor blue, vermilion, bluish green and purple. Orange, sky blue and yellow work best as larger fills, bars and markers.
- **Known limits:** blue vs bluish green is close for tritanopes, and blue vs reddish purple is the closest pair for protanopes. Add markers/dashes whenever both appear.

### Sequential (numeric magnitude, heatmaps)
- **Viridis** (default): `#482475` → `#414487` → `#355F8D` → `#2A788E` → `#21918C` → `#22A884` → `#44BF70` → `#7AD151` → `#BDDF26` → `#FDE725`
- **Cividis** (designed for color vision deficiency, most uniform for everyone): `#00336F` → `#3A4C6C` → `#575C6D` → `#707173` → `#8A8779` → `#A69D75` → `#C4B56C` → `#E4CF5B` → `#FEE838`
- **Single-hue blue:** `#00385C` → `#005485` → `#0B6EAB` → `#0A88CC` → `#35A2E3` → `#5CBAF3` → `#82CEFC` → `#ADDFFF` → `#D4EEFF`

The darkest stops are trimmed so low values don't vanish into the canvas. Low = dark, high = bright, which reads naturally on a dark UI.

### Diverging (values above/below a midpoint)
- **Dark-midpoint blue ↔ orange** (default for dark UIs): `#ADDFFF` → `#5CBAF3` → `#0A88CC` → `#005485` → `#2D3239` → `#734200` → `#B07000` → `#E7A000` → `#FFCF86`
- **ColorBrewer PuOr** (light midpoint, colorblind-safe): `#7F3B08` → `#BE630A` → `#EF9E3C` → `#FED7A2` → `#F6F6F7` → `#CECDE4` → `#988DBE` → `#5D3790` → `#2D004B`
- **ColorBrewer RdBu** (light midpoint, colorblind-safe): `#67001F` → `#BB2A34` → `#E58368` → `#FBCEB7` → `#F6F7F7` → `#C0DCEB` → `#68ABD0` → `#2870B1` → `#053061`

Light-midpoint diverging maps show a bright band in the middle of a dark UI. Use them only inside a card where that's acceptable.

### Alternatives (Paul Tol, colorblind-safe)
- **Tol Bright:** blue `#4477AA`, red `#EE6677`, green `#228833`, yellow `#CCBB44`, cyan `#66CCEE`, purple `#AA3377`, grey `#BBBBBB`
- **Tol Vibrant:** orange `#EE7733`, blue `#0077BB`, cyan `#33BBEE`, magenta `#EE3377`, red `#CC3311`, teal `#009988`, grey `#BBBBBB`

Use these only if Okabe-Ito clashes with a project's brand. Don't mix palettes in one app.

### Never use
Rainbow / jet / HSV colormaps, red-green diverging maps (RdYlGn), and pale pastel categorical sets on dark backgrounds.

---

## 6. Implementing on each platform

- **Web (plain CSS):** import `tokens.css`, use `var(--bg-surface)`, `var(--text-primary)`, etc. Token names are the dotted names with dots replaced by hyphens.
- **Tailwind v4:** import `tailwind-theme.css` after Tailwind. It clears the default palette so only safe colors exist, giving classes like `bg-bg-surface`, `text-text-muted`, `bg-primary-solid text-primary-on`, `border-border-strong`.
- **shadcn/ui:** map `--background` → bg.canvas, `--card` → bg.surface, `--popover` → bg.raised, `--primary` → primary.solid, `--primary-foreground` → primary.on, `--secondary` → bg.active, `--muted` → bg.surface, `--muted-foreground` → text.muted, `--accent` → bg.hover, `--destructive` → danger.solid, `--border` → border.default, `--input` → border.strong, `--ring` → focus.ring.
- **SwiftUI / UIKit:** generate a `Color` extension from `tokens.json` semantic group, set `.preferredColorScheme(.dark)` at the root and `UIUserInterfaceStyle = Dark` in Info.plist.
- **Jetpack Compose:** generate a `darkColorScheme(...)` mapping (primary → primary.solid, onPrimary → primary.on, surface → bg.surface, background → bg.canvas, error → danger.solid, outline → border.strong) plus a custom `CompositionLocal` for status and viz tokens. Don't call `lightColorScheme`.
- **Flutter:** `ThemeData(brightness: Brightness.dark, colorScheme: ColorScheme.dark(...))` with a `ThemeExtension` for status and viz tokens; set `themeMode: ThemeMode.dark`.
- **React Native / Expo:** export `tokens.json` semantic values as a typed `theme.ts`; set `userInterfaceStyle: "dark"` in app config.
- **Qt / PySide / Tauri / Electron:** generate a stylesheet or JS theme object from `tokens.json`; set the window/title bar to dark where the OS allows.
- **Charts (Recharts, Chart.js, ECharts, Plotly, matplotlib):** pass `viz.categorical` in order as the series color list, `chart.grid` for gridlines, `chart.axis` for axes, `chart.label` for tick labels.

---

## 7. QA checklist before shipping a screen

- [ ] No raw hex in components; only semantic tokens.
- [ ] Every status (error, success, warning, info) has an icon and text, not just color.
- [ ] Charts have markers/dashes or direct labels; same series uses same color everywhere.
- [ ] Focus ring visible on every interactive element.
- [ ] Inputs, checkboxes and toggles use `border.strong` (3:1).
- [ ] Viewed the screen through a CVD simulator (Chrome DevTools → Rendering → Emulate vision deficiencies, or Color Oracle / Sim Daltonism) and a grayscale filter. Everything important still reads.
- [ ] No light-theme code was generated.

---

## Sources
- Okabe & Ito, *Color Universal Design: How to make figures and presentations that are friendly to colorblind people* - https://jfly.uni-koeln.de/color/
- Okabe-Ito / Wong palette, ordering and contrast guidance - https://figcanvas.com/blog/okabe-ito-palette-hex-rgb-wong-prism
- Wong, B. (2011). Points of view: Color blindness. *Nature Methods* 8, 441.
- Tol, P. (2021). *Colour Schemes*, SRON/EPS/TN/09-002.
- Viridis & Cividis colormaps (matplotlib); ColorBrewer (Brewer, Penn State).
- WCAG 2.2 contrast criteria 1.4.3 and 1.4.11.
