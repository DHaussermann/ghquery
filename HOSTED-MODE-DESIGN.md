# Hosted Mode — Design

## Overview

ghquery today is a local-install tool. It reads everything from `config.yaml`, writes per-user query and schedule preferences back to that same file, and delegates scheduling to the operating system (launchd on macOS, cron on Linux, Task Scheduler on Windows). Hosted mode turns ghquery into a single shared web app — one always-on instance (target: an AWS `t3.small`) that a dozen teammates use from their browsers, with **no database** and **no per-user OS setup**.

The guiding principle is one codebase, two modes. Local mode behaves exactly as it does today. Hosted mode is activated by an environment variable and changes where configuration comes from and where per-user preferences are stored. The mode itself is part of the configuration.

This document describes the design. It does not cover the Anthropic API migration (a separate foundational change) or the AWS/Terraform deployment work (a later phase).

## Two modes, one codebase

Mode is detected once at startup. Hosted mode is on when `GHQUERY_HOSTED=1` is set in the environment; otherwise the tool runs in local mode. The flag drives three things: where the catalog comes from, whether per-user preferences are written server-side or client-side, and whether scheduling uses the OS scheduler or the in-process scheduler.

Local mode is unchanged in every respect. None of the existing local behavior — config.yaml reads and writes, OS scheduler installation, the CLI — is altered.

## The configuration split

The core of hosted mode is separating configuration into two halves that are tangled together in one `config.yaml` today.

### Global, admin-managed

Identical for every user, set once at deploy time:

- **Catalog** — the repo list, the teams-to-members map, and author display names. Non-sensitive structured data.
- **Secrets** — the GitHub token and the Anthropic API key. Shared across all users.

The catalog is supplied as a **read-only mounted `catalog.yaml`** file. Viper continues to read `catalog.repos`, `catalog.teams`, and `catalog.author_names` exactly as it does today, so this is a near-zero code change. Catalog edits (adding a repo, updating team membership) are done by editing the file and restarting the instance — this is intentional, since the catalog is admin-managed, not something individual users should mutate through the UI.

Secrets are **never written into the catalog file or any on-disk config**. They are injected as environment variables (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`) sourced from AWS Secrets Manager at runtime. This keeps secrets under access control, rotation, and audit, and keeps the catalog file freely editable and reviewable. Because `viper.AutomaticEnv()` is already enabled, `viper.GetString("github_token")` transparently picks up the `GITHUB_TOKEN` environment variable with no parsing code.

The reasoning for keeping secrets out of the catalog file:

- Security — secrets in Secrets Manager get rotation and audit; a plaintext token in a mounted file gets neither and risks ending up in an image layer or git history.
- Separation of concerns — the catalog is boring, shareable config; secrets are not.
- Zero code cost — the environment-variable path already exists.

### Per-user

Different for every user, and the data this design is really about:

- The query recipe — repos, authors, days, mode, and the toggles (`skip_analysis`, `use_coderabbit`).
- The webhook URL — where this user's report is delivered.
- The schedule timing — when a recurring run fires.

In hosted mode these no longer live in `config.yaml`. They live in the user's browser, with one exception for scheduling described below.

## Browser preferences with localStorage

Per-user preferences are stored in the browser's **localStorage** — a persistent, per-origin key/value store that survives browser restarts indefinitely and is never sent to the server unless our code chooses to send it. This is distinct from a cookie (which is size-limited and sent automatically on every request) and from sessionStorage (which is wiped when the tab closes).

Two keys are used:

- `ghquery_prefs` — a single JSON blob holding the saved recurring recipe: `repos, authors, days, mode, skip_analysis, use_coderabbit, webhook_url`.
- `ghquery_uid` — a UUID generated on first visit, used as the server-side record key for scheduling.

### Ephemeral runs vs. saved runs

There are two tiers of run, and the distinction matters:

- **Ephemeral run** — the user builds a query in the form and runs it (preview, or Send to Hook) without saving. Nothing is persisted, anywhere. The user can do this freely for one-off reports.
- **Saved recurring query** — the user clicks Save. This is the explicit action that promotes the current form to "my recurring query." It does two things: writes the recipe to `ghquery_prefs` in localStorage so the browser greets the user with it next time, and POSTs the recipe (with the UUID) to the server, overwriting any existing server-side record for that UUID.

localStorage is written **on Save only** — not on every keystroke or every run. This keeps ephemeral runs from polluting the saved state. The Save button behaves conceptually the same as it does in local mode (an explicit save persists the current form); only the destination changes — localStorage plus a server-side flat file, instead of `config.yaml`.

### First-visit defaults

On a fresh browser with no saved preferences, the form defaults to **all repos checked, but zero teams and zero authors checked**. Defaulting everything to checked would produce an enormous, meaningless query across every author in the catalog. The empty author selection forces the user to pick who they care about before running.

The catalog checkboxes themselves (the list of available repos, teams, and authors) always come from the server's `/api/config` endpoint, which serves the global catalog. localStorage only determines which of those boxes start checked.

### Known limitation

localStorage is tied to one browser on one machine. The same person on their laptop and their phone has two independent stores; preferences do not sync across devices. This is an accepted limitation until a real login is added. Login is deferred pending peer feedback; when it lands, the login identity replaces the UUID as the schedule key and enables cross-device sync.

## Scheduling

Scheduling is the one feature that cannot live purely in the browser. A scheduled job must fire while the user's browser is closed, so localStorage is unreachable at fire time. The server must hold a copy of the recipe and run it on the user's behalf.

This does not require a database. It requires a **flat JSON file per user**, keyed by the browser's UUID, under a server data directory:

```
$GHQUERY_DATA/schedules/<uuid>.json
  → { uuid, recipe{repos, authors, days, mode, toggles}, webhook_url,
      time, frequency, weekday, tz, enabled }
```

### How the webhook URL reaches the server

The webhook URL is one field of the recipe. It lives in localStorage with the rest of the preferences. When the user clicks Save, the browser includes it in the POST body alongside the UUID and the rest of the recipe. The server writes it into the per-UUID flat file. So the webhook URL is linked to the UUID in that file, and it is the delivery target the scheduler uses when the job fires.

For an ephemeral on-demand "Send to Hook," the webhook URL rides in that single request and is not persisted.

### One record per UUID

The same flat file holds both halves of a user's scheduled run:

- The Save action populates the `recipe` and `webhook_url`.
- The schedule modal populates `time`, `frequency`, `weekday`, `tz`, and `enabled`.

Both write the same per-UUID file.

### The in-process runner

An in-process scheduler starts with the server in hosted mode. It evaluates due schedules against each record's stored timezone, and on fire it calls `pipeline.Execute()` in-process with the stored recipe and delivers to the stored webhook URL. This is the same pipeline entry point the CLI `--scheduled` path calls today — only the trigger changes, from launchd/cron to a Go timer.

One improvement over the OS path: the existing OS scheduler ignores the `tz` field and fires in the machine's local time. The in-process runner evaluates against the stored IANA timezone, so a schedule fires at the correct wall-clock time for the user's timezone.

In hosted mode the OS-scheduler code path is not used. In local mode it is untouched.

## API and storage behavior by mode

| Concern | Local mode (today) | Hosted mode |
|---|---|---|
| Catalog source | `config.yaml` | read-only mounted `catalog.yaml` |
| Secrets | `config.yaml` or env | env vars from Secrets Manager |
| Per-user recipe (on-demand) | form → `/api/run` body | form → `/api/run` body (unchanged — already stateless) |
| Per-user recipe (saved) | `viper.WriteConfig()` to `config.yaml` | localStorage + per-UUID flat JSON file |
| Webhook URL (saved) | `config.yaml` | per-UUID flat JSON file |
| Scheduling | OS scheduler (launchd/cron/schtasks) | in-process runner + flat JSON store |
| User identity | n/a (single user) | browser UUID (interim, until login) |

## What does not change

- `/api/run` request handling and the `runRequest` → `pipeline.RunParams` mapping. It is already stateless and recipe-complete; the browser posts the full recipe in the body.
- The pipeline, analysis, and webhook-output code. Hosted mode reuses `pipeline.Execute()` verbatim.
- The OS-scheduler implementations. Still used in local mode.
- The catalog read path in `buildConfigResponse()`. Reused as-is.

## Analysis backend (implemented)

The analysis invoker (`internal/analysis/invoker.go`) supports two backends, selected automatically:

- **Anthropic API** — used when `ANTHROPIC_API_KEY` is set (the hosted-mode path). Both passes call the Messages API via the Go SDK. The stable system prompt (Pass A's enumeration instructions; Pass B's risk-analyzer skill) is marked for prompt caching, so after the first PR in a run the rest read it from cache. The model defaults to `claude-opus-4-8`, overridable with `ANTHROPIC_MODEL`. Per-pass token usage (input / cache_read / output) is logged.
- **claude CLI** — used when no API key is set (local Pro/Max subscription). Unchanged.

This makes the hosted deployment "just add an API key": set `ANTHROPIC_API_KEY` (and optionally `ANTHROPIC_MODEL`) in the environment and analysis runs against the API with no local `claude` install.

## Out of scope for this design

- The Docker image and Terraform deployment (instance, Elastic IP, Secrets Manager, security group, where `catalog.yaml` is mounted from).
- Login and authentication. Replaces the UUID as the schedule key and enables cross-device sync. Deferred.
- Any database. The flat JSON store is a deliberate choice to avoid one.
