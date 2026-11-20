# Page domains

- `LoginPage.tsx` is the public auth entry. It must not acquire protected-shell provider or data dependencies.
- `pricing-templates/DeletePricingTemplateDialog.tsx` only renders deletion/conflict state from the pricing feature's usage and deletion owners. Preserve concrete reference evidence and refetch on stale CAS; do not put another collection/mutation owner in that dialog directory.
