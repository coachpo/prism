# Ban Policy presentation

- Collection/read, CRUD/default mutations, and impact paging belong to the named owners under `../../features/loadbalance/`. `LoadbalanceStrategiesTable.tsx` and `DeleteLoadbalanceStrategyDialog.tsx` render their state and invoke callbacks.
- Preserve explicit default/built-in state and bound-model impact evidence. Deleting an attached/default strategy must expose the replacement or blocking decision supplied by the mutation owner.
- `StrategyPreviewTimeline.tsx` and `strategyValueBadges.tsx` render retry/backoff values in operator units. Keep the explicit strategy fields in the feature schema; do not restore model-level cooldown/failover settings.
