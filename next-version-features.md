# Next Version Features (Out of Scope)


## P0: Host art assets for use with HIGH/MEDIUM/LOW and CR icon in webhook
- chat webhook tables auto-squeeze the Risk Level column when the wider PR / Summary columns claim most of the row width
- Both shortcode emoji (`:large_yellow_circle:`) and Unicode emoji (`🟡`) render with a wrap point that pushes the level text onto a second line in that cell — confirmed empirically across multiple iterations
- Real fix is hosting custom PNG badges for HIGH (red) / MEDIUM (yellow) / LOW (green) plus the CodeRabbit icon, then referencing them via `![alt](url)` markdown
- Hosting options to evaluate:
  - custom chat emoji upload (requires admin) — best UX, single shortcode reference
  - Static endpoint on the ghquery web server, surfaced via public URL — works for local + ngrok but breaks for non-local viewers
  - Public GitHub repo + `raw.githubusercontent.com` URLs — works for everyone but requires public repo
- Until that's solved, the Risk Level cell shows bold text only (`**HIGH**` / `**MED**` / `**LOW**`) and the color cue lives in the header summary line and drilldown sections


## P0: Regression Fault Model
- Build a persistent list of known fragile areas ("fault modes") derived from PR analysis over time
- Each PR analysis becomes a data point — if a change to `server/channels/app/session.go` causes a HIGH risk rating repeatedly, that path gets flagged as a known fragile area
- Feed this data back into the risk-analyzer agent so future PRs touching those paths get scored higher on regression_surface automatically
- QA teams maintain this as institutional knowledge rather than re-discovering it per sprint

## P0: Domain-Specific High Risk Areas
- Extend the risk-analyzer with domain-specific knowledge for areas that are inherently high risk regardless of diff size
- Examples:
  - Authentication (SAML, LDAP, OAuth)
  - Data Retention
  - Compliance Export
  - Burn on Read
  - Data Spillage features
  - etc.
- These should automatically elevate both the categorical level and the data_integrity / security_surface dimension scores
- Could be a configurable section in the skill file or a separate domain knowledge file that gets loaded alongside risk-analyzer.md

## P0: Compare Risk Analysis Models
- Compare our per-PR subagent approach against alternative risk analysis models
- Evaluate accuracy, speed, cost, and scalability tradeoffs
- Pending: reference material from peer's implementation


## More Generous Timeouts for Single-PR URL Mode
- When a PR times out in a batch run it is marked UNKNOWN — acceptable, the batch moves on
- If you specifically want that PR's analysis, re-run it via the single-PR URL field
- In that mode, automatically apply more generous timeouts (e.g. 150s Pass A / 210s Pass B) since only one PR is being analyzed and the result is explicitly wanted
- Batch timeouts stay unchanged — increasing them would raise token consumption and wall-clock time for all scheduled runs
- Implementation: detect `params.PRUrl != ""` in the invoker and use separate timeout constants; no config keys, no pipeline plumbing needed

## Save Last Run as Defaults
- After each run, write the selected repos, authors/teams, days, and mode back to config.yaml as new defaults
- Next run pre-fills with whatever you used last time — no re-typing
- Keeps the interactive prompt but makes repeat queries instant (just press Enter through)

## Opt-in Querying for Authors Outside Config
- Add a flag (e.g. `--allow-external`) that explicitly permits querying GitHub usernames not listed in config.yaml teams
- Without the flag, unknown authors are rejected as they are today
- Prevents accidental queries while still allowing ad-hoc lookups when intentional

## Configurable Claude Model
- Add `model` key to config.yaml (e.g. `claude-sonnet-4-6[1m]`)
- Pass value to `claude -p` via `--model` flag in the analysis invoker
- Allows switching models without code changes

## GitHub App Integration
- Real-time PR webhook processing (like CodeRabbit)
- Automatic inline review comments on PRs
- Status checks integration
