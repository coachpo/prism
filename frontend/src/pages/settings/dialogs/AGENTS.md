# Settings dialogs

- Keep rule/delete/retention dialog rendering here. Parent settings resource hooks own reads, mutation state, preflight validity, and save orchestration.
- `RetentionPolicyPreflightDialog.tsx` and `DeleteConfirmDialog.tsx` display the server's fresh impact evidence and `confirmation_keyword`. Never replace the keyword with localized copy/a client constant, or show a valid typed confirmation after its preflight is discarded.
- Confirmation stays disabled when preflight semantics are incomplete or submission is active. Preserve displayed count accuracy, coverage bounds, and expiry instead of reducing preflight to a row count.
- Audit-rule editors render field errors and invoke parent callbacks; do not add inline modal branches or duplicate mutation orchestration in `../../SettingsPage.tsx`.
