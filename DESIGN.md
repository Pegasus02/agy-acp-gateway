# `:18891` Console UI Notes (`DESIGN.md`)

Style and component rules for `go-gateway/webui/dashboard.html`. All CSS, JS, and Lucide SVG icons (`go-gateway/webui/lucide-icons.json`) are embedded into the Go binary with no external network requests.

## CSS Variables (`:root`)

- **Surfaces**: `--paper: #f3efe7` (page), `--card: #fdfcf9` (top-level `.module` / `.acc-modal`), `--paper-hi: #f8f6f1` (nested `.inst-qcard` / `.sess` / `.acc-item`), `--paper-dim: #eae5da` (tracks / code tags), `--surface-pure: #ffffff` (inputs / buttons / active tabs).
- **Text & Borders**: `--ink: #15171c`, `--ink-2: #444854`, `--ink-3: #646976`, `--ink-4: #9ea4b0`, `--line: rgba(20, 22, 27, 0.075)`, `--line-2: rgba(20, 22, 27, 0.13)`.
- **Accent & Model Series**: `--accent` / `--gem: #2451e6`, `--cla: #c85226`, `--gpt: #0e8562`, `--c-think: #863899`, `--c-cache: #148058`.
- **Status**: `--ok: #147a52`, `--warn: #a35f0b`, `--bad: #c1322b` (paired with `--*-soft` `9-10%` fills).
- **Fonts**: `--sans` (UI body/headings), `--display` (KPI numbers), `--mono` (timestamps, IDs, keys, ratios, USD). Always set `font-variant-numeric: tabular-nums` on numbers.

## Layout & Hierarchy

- **Hide default-healthy noise**: Shared healthy info (like the active `agy` version) appears once in the top bar. Account cards only show badges for non-default states (`备用`, `共享`, version mismatch, cooldown, `需要验证`, or safety-check limit).
- **One dark solid button per view**: Reserve `.btn.solid` for `同步`, modal saves (`保存配置`), and unauthenticated login prompts (`网页授权`). Filters and tabs use white buttons inside `--paper-dim` tracks.
- **Quota grid alignment**: `.quota-multi` uses 2 equal columns (`1fr` at `<= 960px`). Keep `flex: 0 0 auto` and `gap: 8px` on `.inst-qrows` with `margin-top: auto` on `.inst-qfoot` so quota bars line up between Pro and Ultra cards.
- **Collapsible quota pool**: Collapse state is stored in `localStorage` (`acp.quotaCollapsed`). When collapsed, `#quota-mini-bar` shows compact account chips above the fold.

## Corner Radii & Controls

- **4 radius steps**: `--r-xs: 6px` (inline tags/badges), `--r-sm: 8px` (buttons, `36px` inputs, `.ui-sel-trigger`, `.acc-mchk`, `.acc-switch-row`), `--r-md: 12px` (nested cards, `.pool-ctrl-bar`, `.ui-sel-menu`), `--r-lg: 16px` (`.module`, `.acc-modal`).
- **Nested inner radii**: `10px` `.seg` track -> `7px` active button; `12px` `.ui-sel-menu` -> `7px` option; `8px` `.acc-mchk` -> `4.5px` checkbox; `999px` / `50%` only on status dots, progress bars, and `.ui-switch`.
- **Custom controls**:
  - `.ui-sel` + `#ui-sel-menu`: Replaces native `<select>` popups while keeping the underlying `<select>` synced for change events and keyboard navigation.
  - Checkbox & `.ui-switch`: `15x15px` `appearance: none` checkbox (`chkPop` `150ms`) for model lists; `36x20px` switch for boolean account flags (`共享账号`).
  - Inputs: Text inputs use `var(--sans)`; IDs, keys, URLs, and numbers use `var(--mono)`.

## Hard Constraints

- Load all icons from `go-gateway/webui/lucide-icons.json` via `svg()` or `data-ic`.
- Never use Emoji characters or em/en dashes (`U+2014` / `U+2013`) in `dashboard.html` (rejected by Go unit tests).
