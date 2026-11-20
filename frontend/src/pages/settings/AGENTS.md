# Settings orchestration

- `settingsNavigation.ts` owns the `scope=global|instance` URL contract, section allowlists, and defaults. Keep hash/section focus in `useSettingsPageSectionState.ts`; old `tab` input is removed during canonicalization.
- Global scope contains reporting currency/timezone and audit/privacy controls. Instance scope contains authentication, retention, manual cleanup, and server-persisted jobs. Keep that visible meaning even though `SettingsProfileTab.tsx` and `SettingsGlobalTab.tsx` retain older internal names.
- `useAuditConfigurationData.ts` composes API-family audit policy, header blocklist, and User-Agent Client Rule lifecycles; do not fold them into one request state.
- Destructive changes use a fresh server preflight/confirmation and server-persisted job; browser state is never job truth.
- Job list and detail evidence remain separate read lanes. Retain opaque cursor consistency, manual refresh, post-mutation calibration, and cancellation in `useRetentionJobList.ts`/`useRetentionJobDetails.ts`.
- `SettingsSaveAction.tsx` and `sectionSaveState.tsx` own dirty/saving/saved feedback; do not create independent inline saves or toast-only truth in sections.
- Keep bootstrap file editing out of Settings.
