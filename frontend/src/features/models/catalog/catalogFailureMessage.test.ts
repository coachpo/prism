import { describe, expect, it } from "vitest";
import { getStaticMessages } from "@/i18n/staticMessages";
import { catalogFailureMessage } from "./catalogFailureMessage";

describe("catalog user errors", () => {
  it("keeps known recovery messages and never returns unknown diagnostics", () => {
    const copy = getStaticMessages();
    expect(catalogFailureMessage(new Error("SELECT * FROM catalog at /internal/catalog.go:99"))).toBe(copy.common.requestErrors.unknown);
    expect(catalogFailureMessage({ status: 409 })).toBe(copy.common.requestErrors.changed);
    expect(catalogFailureMessage(new Error(copy.modelExportPage.sourceReconciliationFailed))).toBe(copy.modelExportPage.sourceReconciliationFailed);
    expect(catalogFailureMessage(new Error("pi_catalog_search_identity_changed: runtime mismatch"))).toBe(copy.modelExportPage.directorySearchEvidenceChanged);
  });
});
