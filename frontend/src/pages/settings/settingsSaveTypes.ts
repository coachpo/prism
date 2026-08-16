// `costing` covers the merged basis-and-display card: reporting currency and
// timezone were always one `PUT /api/settings/costing`, so they share one
// dirty state and one save.
export type SettingsSaveSection = "costing" | "audit" | "retention";
