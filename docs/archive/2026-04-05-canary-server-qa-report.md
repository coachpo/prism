# 2026-04-05 canary server QA report

## Scope

- Target: `http://192.168.1.222:8088`
- Mode: live exploratory browser QA with non-destructive coverage only
- Focus: route availability, navigation, dialogs, drill-downs, filter behavior, and obvious functional regressions

## Coverage summary

This pass exercised both route availability and user-visible branches. The sections below record what was actually opened or changed, not just what rendered.

### Route and branch coverage

- Root route: `/` redirected to `/dashboard`.
- Dashboard: opened the landing shell, observed quick actions, and confirmed the shell recovered normally after later control changes.
- Statistics: switched time windows and exercised the export action.
- Request Logs: exercised browse mode, `ingress_request_id` filtering, exact-request mode via `request_id`, clear-filters behavior, and model deep-link entry from model detail.
- Models: opened the models list, navigated into model detail, opened the load-balance events tab, used the request-log deep link, and opened the **New Model** dialog.
- Proxy model detail route: `/models/1/proxy` resolved into the proxy-model detail page with connection cards, monitoring state, spend overlays, and connection-management actions.
- Monitoring: exercised the monitoring overview plus vendor and model drill-down routes.
- Endpoints: opened the endpoints list, confirmed data hydration after initial loading, and opened the **Add Endpoint** dialog.
- Loadbalance Strategies: opened the strategies list, confirmed both legacy and adaptive strategies rendered, and opened the **Add Loadbalance Strategy** dialog.
- Pricing Templates: confirmed the empty-state route, opened the **Add Pricing Template** dialog, and exercised the missing-special-token-policy selector.
- Settings: exercised both **Profile** and **Global** tabs, inspected Authentication, Backup, Billing & Currency, Timezone, Audit & Privacy, Retention & Deletion, and opened the **Add Vendor** dialog.
- Proxy API Keys: confirmed the empty-state route, filled the create-key form fields non-destructively, and verified the authentication-off guidance.

### Public auth-route behavior

- `/login`
- `/forgot-password`
- `/reset-password`

All three routes resolved into the protected dashboard shell during this run instead of rendering standalone auth screens. Based on the observed canary state, this looked like auth-disabled or already-authorized behavior rather than a route failure.

### Shell-control coverage

- Language menu: switched the live shell from English to Simplified Chinese and then back to English.
- Theme menu: switched the live shell to dark mode and then back to light mode.
- Profile selector: opened the profile dialog and verified search plus management actions were present.

### Route loading behavior

Multiple routes briefly showed `Loading application...` before resolving into usable content, including Models, Endpoints, Loadbalance Strategies, Pricing Templates, Proxy API Keys, and Settings. During this run those routes recovered without crashing and were not treated as defects by themselves.

### Dialog and form behaviors exercised

- **New Model** dialog: typing `canary-temp-model` into **Model ID** auto-populated **Display Name** with the same value before the dialog was canceled.
- **Add Connection** dialog on `/models/1/proxy`: opened successfully and exposed both endpoint-source tabs, pricing template selection, probe controls, custom headers, and connection test/save actions.
- **Add Endpoint** dialog: opened successfully and accepted a non-destructive name draft before closing.
- **Edit Endpoint** dialog: opened successfully from an existing endpoint card and exposed the persisted name/base URL plus the API-key replacement field.
- **Add Loadbalance Strategy** dialog: opened successfully, exposed both **Legacy strategy** and **Adaptive strategy** families, and switched into the adaptive form before closing.
- **Edit Loadbalance Strategy** dialog: opened successfully for the legacy default strategy and exposed existing recovery, failure-code, and ban-mode fields.
- **Add Pricing Template** dialog: opened successfully, accepted a non-destructive name draft, and exposed the missing-special-token-policy list with `Map to Output Price` and `Zero Cost` options.
- **Create proxy key** form: accepted non-destructive Name and Notes input without submission.
- **Add Vendor** dialog: opened successfully from the global settings vendor-management section.

## Confirmed defect

### Request Logs request-lookup draft is not synchronized with filter actions

**Area**

- Live route: `/request-logs`
- Frontend seam: `frontend/src/pages/request-logs/FiltersBarPrimaryFilters.tsx`

**Steps to reproduce**

1. Open `/request-logs`.
2. Type a numeric value such as `42` into the **Request ID** field, but do not press Enter.
3. Type any value into **Ingress request ID** so the page enters an active browse-filter state.
4. Click **Clear Filters**.

**Expectation**

- Visible toolbar state should stay synchronized with the actual page state.
- If the request lookup is still only a draft, clearing filters should clear the visible draft as well.
- The page should not leave a visible Request ID value that is no longer represented in the URL or active results.

**Reality**

- Clear Filters removes the URL-backed browse filters.
- The Request ID field still displays the previously typed value even though exact-request mode never activated and the URL no longer contains that request lookup.

**Gap / likely cause**

- `FiltersBarPrimaryFilters.tsx` stores the Request ID input in local component state (`requestLookupValue`).
- Browse filters and exact-request mode are driven by URL-backed state from `useRequestLogPageState.ts`.
- Because the draft input is not derived from URL state, clearing browse filters can leave stale visible text that does not match the active page state.

**Notes**

- Exact-request mode itself works for numeric IDs when `request_id` is present in the URL. For example, `/request-logs?request_id=42` issued `GET /api/stats/requests/42` and rendered the expected not-found state for a missing request.
- The defect is limited to the toolbar draft state, not the exact-request route contract itself.

## Additional issue surfaced during dialog inspection

### Models dialog accessibility warnings in browser diagnostics

**Area**

- Live route: `/models`
- Surface: **New Model** dialog

**Steps to reproduce**

1. Open `/models`.
2. Click **New Model**.
3. Inspect browser issues after the dialog opens.

**Expectation**

- Form fields in the dialog should expose accessible labeling and stable form attributes.

**Reality**

- Chrome DevTools reported accessibility issues when the dialog was open, including `No label associated with a form field (count: 3)` and `A form field element should have an id or name attribute (count: 4)`.

**Gap / likely cause**

- At least some dialog controls appear to be rendered without the full label/id-or-name wiring expected by browser accessibility diagnostics.
- This pass did not isolate the exact offending controls, so follow-up should inspect the dialog fields one by one.
