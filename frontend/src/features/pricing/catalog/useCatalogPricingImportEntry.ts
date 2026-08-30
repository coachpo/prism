import { useCallback, useState } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";

import type { CatalogPricingSource } from "./useCatalogPricingImport";
import { useCatalogImportReferenceData } from "./useCatalogImportReferenceData";

export interface UseCatalogPricingImportEntryResult {
 /** True once the operator has resolved an offering; gates preview + commit. */
 resolvedSource: CatalogPricingSource | null;
 catalogImportOpen: boolean;
 closeCatalogImport: () => void;
 openCatalogImport: () => void;
 reference: ReturnType<typeof useCatalogImportReferenceData>;
 setResolvedSource: (source: CatalogPricingSource | null) => void;
}

/**
 * Owns the /route/pricing "import from catalog" dialog state.
 *
 * The entry only tracks which dialog step the operator is on. Discovery,
 * preview, and commit stay owned by the shared catalog components, so the
 * pricing page and the model detail action cannot diverge on what a preview
 * promises.
 */
export function useCatalogPricingImportEntry(
 revision: number,
): UseCatalogPricingImportEntryResult {
 const [catalogImportOpen, setCatalogImportOpen] = useState(false);
 const [resolvedSource, setResolvedSource] =
  useState<CatalogPricingSource | null>(null);
 const reference = useCatalogImportReferenceData(revision, catalogImportOpen);

 const openCatalogImport = useCallback(() => {
  setResolvedSource(null);
  setCatalogImportOpen(true);
 }, []);

 const closeCatalogImport = useCallback(() => {
  setCatalogImportOpen(false);
  setResolvedSource(null);
 }, []);

 return {
  catalogImportOpen,
  closeCatalogImport,
  openCatalogImport,
  reference,
  resolvedSource,
  setResolvedSource,
 };
}

/**
 * What a successful catalog import has to make consistent again: the pricing
 * template collection, the Terminal Target option cache that carries template
 * references, and any model-detail target read. Dropping the shared caches
 * forces the next read to be authoritative instead of optimistically patched.
 */
export async function refreshAfterCatalogPricingCommit(
 revision: number,
 refetchPricingTemplates: (forceRefresh?: boolean) => void | Promise<void>,
): Promise<void> {
 clearSharedReferenceData("pricingTemplates", revision);
 clearSharedReferenceData("connections", revision);
 await refetchPricingTemplates(true);
}

/** The toast copy for one completed import, which distinguishes a
 *  template-only write from an assignment so the operator knows what moved. */
export function announceCatalogPricingCommit(
 templateName: string,
 assignedCount: number,
): void {
 const messages = getStaticMessages();
 toast.success(
  messages.modelCatalog.catalogImportSuccessToast(templateName, assignedCount),
 );
}
