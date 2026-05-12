# FRONTEND SETTINGS DIALOG CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/dialogs/` owns the local dialog cluster mounted by `../../SettingsPage.tsx`: destructive confirmations, vendor CRUD modals, and audit-rule editors or delete flows. Keep dialog rendering and per-form copy here while the parent settings hooks own mutation state and orchestration.

## STRUCTURE
```
dialogs/
├── DeleteConfirmDialog.tsx
├── DeleteRuleConfirmDialog.tsx
├── DeleteUserAgentClientRuleConfirmDialog.tsx
├── DeleteVendorDialog.tsx
├── RuleDialog.tsx
├── UserAgentClientRuleDialog.tsx
└── VendorDialog.tsx
```

## WHERE TO LOOK

- Mounted dialog surface and parent handoff: `../../SettingsPage.tsx`, `../AGENTS.md`
- Shared delete-confirm flow for destructive settings actions: `DeleteConfirmDialog.tsx`
- Audit-rule create, edit, and delete flows: `RuleDialog.tsx`, `DeleteRuleConfirmDialog.tsx`, `UserAgentClientRuleDialog.tsx`, `DeleteUserAgentClientRuleConfirmDialog.tsx`
- Shared vendor create, edit, and delete flows: `VendorDialog.tsx`, `DeleteVendorDialog.tsx`
- Mutation state, selected-profile labels, and save orchestration feeding the dialogs: `../useSettingsPageData.ts`, `../useAuditConfigurationData.ts`, `../useRetentionDeletionData.ts`, `../useVendorManagementData.ts`

## CONVENTIONS

- When doing upgrade work, first account for this project stage: This application is under development, it doesn't have users at the moment. Backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested; prefer the best current implementation shape over preserving the old one, and do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.
- Keep dialog files focused on rendering, field composition, and confirm-copy framing.
- Keep mutation state, delete keywords, and save orchestration in the parent settings hooks.
- Keep audit-rule dialogs separate from vendor CRUD dialogs; they share the mount point but not the form contract.

## ANTI-PATTERNS

- Do not add inline modal branches back to `../../SettingsPage.tsx` or the section components when this cluster already owns the dialogs.
- Do not duplicate delete-confirm or save orchestration logic inside the dialog files.
- Do not collapse vendor CRUD and audit-rule editing into one generic dialog contract.
