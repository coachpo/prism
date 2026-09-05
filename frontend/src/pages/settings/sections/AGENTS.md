# Settings section rendering

- Render resource state supplied by the parent settings hooks. Keep section IDs aligned with `../settingsNavigation.ts`; `BasisAndDisplaySection.tsx` contains the retained `timezone` anchor within the combined currency/timezone card.
- `AuthenticationSection.tsx` owns the account card/save handoff. `authentication/AuthenticationFieldShell.tsx` supplies field framing, and `authentication/authenticationPassword.ts` owns password bounds/localized validation; fields and hooks must reuse it.
- Audit configuration sections render independent API-family, header-blocklist, and User-Agent rule owners. They do not establish another settings cache or mutation lifecycle.
- Retention/manual cleanup render server coverage and fresh preflight state; `RetentionJobsSection.tsx` renders the server job snapshot and independent details/evidence from parent hooks. Unknown counts/coverage stay visibly unknown.
- Reporting-currency/migration rendering belongs to [billing-currency](billing-currency/AGENTS.md). Its ordinary save is shared with timezone through the parent costing hook; the section's own actions open migration/archive dialogs rather than create a second save path.
