# Phase 7 (Provider/Customer Portals) — Open Questions

Judgment calls made while planning this phase, and real gaps intentionally
left for whoever implements it to resolve or accept — in the same spirit
as `docs/00-core-platform/OPEN_QUESTIONS.md`: documented honestly, not
silently decided or silently skipped.

## Decided in `PLAN.md`, flagged here for visibility

- **Session-over-bearer-token auth, not OAuth/email/password.** See
  `PLAN.md` §4. This is the single biggest interpretive call in this
  plan: the user's request for "professional" portals could also
  reasonably mean traditional email+password accounts. This plan chose
  to extend the existing "bearer tokens in a table" model with a normal
  web session on top, since `docs/00-core-platform/IMPLEMENTATION.md` §2
  explicitly lists email/OAuth/password as non-goals and nothing in the
  new request said to reverse that. If that reading is wrong, this is
  the one decision to revisit before writing any code against §4–§7.
- **New `provider` token kind** (`PLAN.md` §4.1) rather than reusing
  `agent` tokens for portal login. Adds a migration and a small amount of
  code for a real UX/security improvement (a login credential and a
  machine-registration credential look identical otherwise); flagged in
  case a reviewer would rather minimize schema churn and accept the
  confusion.
- **Hand-written REST/JSON over grpc-web** (`PLAN.md` §5). Right call for
  today's ~19-endpoint surface; revisit if it grows substantially or if
  `lcctl` itself ever wants a browser-embeddable mode (it doesn't today).
- **React + TypeScript + Vite + Tailwind, embedded via `go:embed`**
  (`PLAN.md` §9.1). The one new toolchain dependency in an otherwise
  Go-only repo. The alternative (extending the current build-free vanilla
  JS approach to two full portals) was rejected as not realistically
  maintainable at this scope, but it's a real tradeoff worth a second
  opinion before task 7.11 if whoever implements this disagrees.

## Left for the implementer to decide (marked as such in `IMPLEMENTATION.md`)

- **Task 7.7:** whether `portalapi` calls into `api.CustomerServer`'s
  logic via an extracted shared package, or via direct method calls with
  a constructed context — genuinely depends on what the real code looks
  like once you're in it. Whichever is chosen, the constraint is fixed:
  no duplicated validation logic.
- **Task 7.6:** what a provider account's starting balance should be
  (providers earn, they don't obviously need to start funded the way the
  demo customer account does) — pick something and say why.
- **Task 7.12:** Storybook vs. a lightweight `/dev/components` route for
  visually checking the shared component library. Either is fine; pick
  based on how much the implementer already knows Storybook.

## Real gaps this phase does not close

- **No rate limiting anywhere in the coordinator**, portal login included
  — true of the existing gRPC bearer auth too, not a regression this
  phase introduces, but worth naming rather than letting "we added a
  login form" imply it was addressed. A token-guessing attack is
  infeasible today only because tokens are 32 random bytes
  (`randomToken` in `internal/coordinator/api/customer_server.go`), not
  because of any rate limit.
- **No session revocation UI** ("sign out all other sessions," "see your
  active sessions") — `RevokeSession` exists at the store layer (task
  7.2) but nothing in either portal's UI calls it for anything but the
  current session's own logout. A reasonable phase 8 addition, not
  claimed as done here.
- **No account recovery.** Lose your token (customer or provider), lose
  the account — identical to today's bearer-token model, just more
  visible now that there's a login screen implying "forgot password"
  might exist. Worth a plainly-worded warning in the UI at signup, not a
  feature to half-build.
