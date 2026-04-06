# main-after-v0.2.9 change summary

This archive note captures the plain-language summary of what changed on `main-after-v0.2.9` relative to its base. It is historical support material, not the source of truth for current product behavior.

## Dashboard, statistics, models, and request logs

- Removes repeated provider names in the model list.
- Makes the main app layout wider.
- Widens and rebalances the Dashboard cards and sections.
- Splits Dashboard into **Overview** and **Analytics** tabs.
- Adds an **Analytics** tab with the main usage and statistics breakdowns.
- Keeps the selected Dashboard tab in the address bar.
- Updates Dashboard wording.
- Widens the model search area.
- Widens the request-log filters and rebalances the request-log summary area.
- Tightens the model column in request logs.
- Standardizes the copy buttons in request-log details.
- Widens and rebalances the statistics controls, tables, and loading view.
- Marks Statistics as an advanced view.
- Shows which models are included in usage totals and statistics.

## Providers, endpoints, saved setups, and traffic balancing

- Renames vendor management to provider management.
- Replaces vendor lists with provider lists.
- Moves provider assignment from models to endpoints.
- Makes provider use, delete, and import behavior follow endpoints instead of models.
- Starts with provider-based defaults instead of vendor-based defaults.
- Uses provider management in the app’s startup/setup path instead of vendor management.
- Makes “activate this setup” act directly on the setup you picked.
- Gives header blocking rules their own management area.
- Separates provider-list import/export from full saved-setup import/export.
- Gives traffic-balancing settings and current traffic status their own separate areas.
- Supports two traffic-balancing modes: **legacy** and **adaptive**.
- Starts the default profile with one default option from each traffic-balancing mode.
- Prevents the same settings change from being applied twice if the action is submitted again.
- Does the same for proxy API key changes.

## Model request rules and OpenAI connection checks

- Adds request rules inside model settings so only matching connections are used for a request.
- Lets you create and edit those request rules in model settings.
- Shows those request rules on the model overview and detail screens.
- Rejects the request cleanly when none of the rules match.
- Fixes request-rule editing on the model detail page.
- Restores the OpenAI connection-check choice when adding or editing an OpenAI connection.
- Keeps that choice in previews, saved setups, and import/export.
- Adds default values for that choice.
- Restores its preview behavior and wording.

## Recovery, live updates, and shared UI behavior

- Makes automatic recovery more cautious before putting failed connections back into use.
- Simplifies the older default recovery settings.
- Falls back safely if the real-time connection address is invalid.
- Makes sidebar behavior more consistent.
- Makes request-log detail panels more consistent.
- Makes endpoint delete dialogs more consistent.
- Makes pricing-template delete dialogs more consistent.

## Request handling

- Adds per-model request settings for request time limits and streaming time limits.
- Replaces the old request timeout/budget controls with those settings.
- Keeps those settings when saving, importing, and exporting setups.
- Uses those settings while requests run.
- Makes streamed replies finish only after real output starts arriving.

## Other repo changes

- Adds a deploy script.
- Updates README and the main API, architecture, and data-model docs to match these changes.
- Removes outdated documentation notes.

## Related live docs

- `docs/ARCHITECTURE.md`
- `docs/API_SPEC.md`
- `docs/DATA_MODEL.md`
