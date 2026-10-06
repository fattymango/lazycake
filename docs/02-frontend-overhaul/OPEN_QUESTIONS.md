# Phase 8 — Open Questions and Judgment Calls

Log each real decision here with the date, the options, and why one won.

- **Tailwind v3 vs v4** (2026-10-06): staying on v3. v4 is a build-system
  migration and this phase is about design. Tokens are plain CSS variables
  precisely so the later move is mechanical.
- **Component library vs in-repo primitives** (2026-10-06): in-repo, with
  Radix for the four accessibility-hard widgets (dialog, dropdown, tabs,
  tooltip). A full library brings its own look, which is the opposite of the
  goal, and a large dependency tree into a single-binary product.
- **Fonts** (2026-10-06): self-hosted via `@fontsource` rather than Google
  Fonts, so the UI works offline and the deploy has no third-party request.
- **Default theme** (2026-10-06): dark, following the OS setting if the user
  has one. Open: whether a light default is better for the owner's
  presentations on projectors — easy to flip, tokens exist for both.
