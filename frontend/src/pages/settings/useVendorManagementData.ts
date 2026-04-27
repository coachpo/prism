import { type ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { z } from "zod";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { VendorCatalogImportSchema } from "@/lib/configImportValidation";
import { getSharedVendors, setSharedVendors } from "@/lib/referenceData";
import type {
  Vendor,
  VendorCatalogImportPreviewResponse,
  VendorCatalogImportRequest,
  VendorModelUsageItem,
} from "@/lib/types";
import { toast } from "sonner";
import {
  DEFAULT_VENDOR_FORM,
  normalizeVendorPayload,
  vendorFormStateFromVendor,
  type VendorFormState,
} from "./vendorManagementFormState";

interface UseVendorManagementDataInput {
  bumpRevision: () => void;
  revision: number;
}

function buildVendorCatalogExportFilename(now: Date = new Date()) {
  const date = now.toISOString().split("T")[0];
  return `prism-vendor-catalog-v1-${date}.json`;
}

export function useVendorManagementData({ bumpRevision, revision }: UseVendorManagementDataInput) {
  const [vendors, setVendors] = useState<Vendor[]>([]);
  const [vendorsLoading, setVendorsLoading] = useState(false);
  const [vendorDialogOpen, setVendorDialogOpen] = useState(false);
  const [editingVendor, setEditingVendor] = useState<Vendor | null>(null);
  const [vendorForm, setVendorForm] = useState<VendorFormState>(DEFAULT_VENDOR_FORM);
  const [vendorSaving, setVendorSaving] = useState(false);
  const [deleteVendorConfirm, setDeleteVendorConfirm] = useState<Vendor | null>(null);
  const [deleteVendorDialogOpen, setDeleteVendorDialogOpen] = useState(false);
  const [displayedDeleteVendorConfirm, setDisplayedDeleteVendorConfirm] = useState<Vendor | null>(null);
  const [deleteVendorConflict, setDeleteVendorConflict] = useState<VendorModelUsageItem[] | null>(null);
  const [vendorDeleting, setVendorDeleting] = useState(false);
  const [vendorUsageLoading, setVendorUsageLoading] = useState(false);
  const [vendorUsageRows, setVendorUsageRows] = useState<VendorModelUsageItem[]>([]);
  const [catalogExporting, setCatalogExporting] = useState(false);
  const [catalogImporting, setCatalogImporting] = useState(false);
  const [catalogSelectedFile, setCatalogSelectedFile] = useState<File | null>(null);
  const [catalogParsedImport, setCatalogParsedImport] = useState<VendorCatalogImportRequest | null>(null);
  const [catalogPreviewResult, setCatalogPreviewResult] = useState<VendorCatalogImportPreviewResponse | null>(null);
  const catalogFileInputRef = useRef<HTMLInputElement>(null);
  const currentCatalogSelectionTokenRef = useRef(0);

  const commitVendors = useCallback(
    (updater: (current: Vendor[]) => Vendor[]) => {
      setVendors((current) => {
        const next = updater(current);
        setSharedVendors(revision, next);
        return next;
      });
    },
    [revision],
  );

  const resetCatalogImportState = useCallback(() => {
    setCatalogSelectedFile(null);
    setCatalogParsedImport(null);
    setCatalogPreviewResult(null);
    if (catalogFileInputRef.current) {
      catalogFileInputRef.current.value = "";
    }
  }, []);

  const catalogImportSummary = useMemo(
    () => ({
      createCount: catalogPreviewResult?.create_count ?? 0,
      updateCount: catalogPreviewResult?.update_count ?? 0,
      vendorCount: catalogParsedImport?.vendors.length ?? 0,
    }),
    [catalogParsedImport, catalogPreviewResult],
  );

  const fetchVendors = useCallback(async () => {
    const messages = getStaticMessages();
    setVendorsLoading(true);
    try {
      const data = await getSharedVendors(revision);
      setVendors(data);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsAuditData.loadVendorsFailed);
    } finally {
      setVendorsLoading(false);
    }
  }, [revision]);

  useEffect(() => {
    void fetchVendors();
  }, [fetchVendors]);

  const handleCatalogExport = async () => {
    const messages = getStaticMessages();

    setCatalogExporting(true);
    try {
      const data = await api.config.vendors.export();
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = buildVendorCatalogExportFilename();
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(messages.vendorManagement.catalogExportSucceeded);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.vendorManagement.catalogExportFailed);
    } finally {
      setCatalogExporting(false);
    }
  };

  const handleCatalogFileSelect = async (event: ChangeEvent<HTMLInputElement>) => {
    const messages = getStaticMessages();
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }

    const selectionToken = currentCatalogSelectionTokenRef.current + 1;
    currentCatalogSelectionTokenRef.current = selectionToken;
    setCatalogSelectedFile(file);
    setCatalogParsedImport(null);
    setCatalogPreviewResult(null);

    try {
      const text = await file.text();
      if (currentCatalogSelectionTokenRef.current !== selectionToken) {
        return;
      }
      const parsed = JSON.parse(text);
      const validation = VendorCatalogImportSchema.safeParse(parsed);

      if (!validation.success) {
        const errors = validation.error.issues
          .map((issue: z.ZodIssue) => `${issue.path.join(".")}: ${issue.message}`)
          .join(", ");
        throw new Error(messages.vendorManagement.catalogInvalidPayload(errors));
      }

      const catalogImport = validation.data as VendorCatalogImportRequest;
      const preview = await api.config.vendors.previewImport(catalogImport);
      if (currentCatalogSelectionTokenRef.current !== selectionToken) {
        return;
      }
      setCatalogParsedImport(catalogImport);
      setCatalogPreviewResult(preview);
      if (!preview.ready && preview.blocking_errors.length > 0) {
        toast.error(preview.blocking_errors[0]);
      }
    } catch (error) {
      if (currentCatalogSelectionTokenRef.current !== selectionToken) {
        return;
      }
      toast.error(error instanceof Error ? error.message : messages.vendorManagement.catalogInvalidJsonFile);
      resetCatalogImportState();
    }
  };

  const handleCatalogImport = async () => {
    const messages = getStaticMessages();
    if (!catalogParsedImport || !catalogPreviewResult?.ready) {
      if (catalogPreviewResult && catalogPreviewResult.blocking_errors.length > 0) {
        toast.error(catalogPreviewResult.blocking_errors[0]);
      }
      return;
    }

    setCatalogImporting(true);
    try {
      const result = await api.config.vendors.import(catalogParsedImport);
      const nextVendors = await api.vendors.list();
      setVendors(nextVendors);
      setSharedVendors(revision, nextVendors);
      toast.success(
        messages.vendorManagement.catalogImportSucceeded(
          String(result.created_count),
          String(result.updated_count),
        ),
      );
      resetCatalogImportState();
      bumpRevision();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.vendorManagement.catalogImportFailed);
    } finally {
      setCatalogImporting(false);
    }
  };

  const closeVendorDialog = () => {
    setVendorDialogOpen(false);
  };

  const closeDeleteVendorDialog = () => {
    setDeleteVendorDialogOpen(false);
    setDeleteVendorConfirm(null);
    setDeleteVendorConflict(null);
  };

  const openCreateVendorDialog = () => {
    setEditingVendor(null);
    setVendorForm(DEFAULT_VENDOR_FORM);
    setVendorDialogOpen(true);
  };

  const handleEditVendor = (vendor: Vendor) => {
    if (vendor.is_readonly) {
      return;
    }
    setEditingVendor(vendor);
    setVendorForm(vendorFormStateFromVendor(vendor));
    setVendorDialogOpen(true);
  };

  const handleSaveVendor = async () => {
    const messages = getStaticMessages();
    const payload = normalizeVendorPayload(vendorForm);

    if (!payload.key) {
      toast.error(messages.vendorManagement.vendorKeyRequired);
      return;
    }

    if (!payload.name) {
      toast.error(messages.vendorManagement.vendorNameRequired);
      return;
    }

    setVendorSaving(true);
    try {
      if (editingVendor) {
        const updatedVendor = await api.vendors.update(editingVendor.id, payload);
        commitVendors((current) =>
          current.map((vendor) => (vendor.id === editingVendor.id ? updatedVendor : vendor)),
        );
        toast.success(messages.vendorManagement.vendorUpdated);
      } else {
        const createdVendor = await api.vendors.create(payload);
        commitVendors((current) => [createdVendor, ...current]);
        toast.success(messages.vendorManagement.vendorCreated);
      }

      closeVendorDialog();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.vendorManagement.vendorSaveFailed);
    } finally {
      setVendorSaving(false);
    }
  };

  const handleDeleteVendorClick = async (vendor: Vendor) => {
    if (vendor.is_readonly) {
      return;
    }
    const messages = getStaticMessages();
    setDeleteVendorConfirm(vendor);
    setDisplayedDeleteVendorConfirm(vendor);
    setDeleteVendorDialogOpen(true);
    setDeleteVendorConflict(null);
    setVendorUsageRows([]);
    setVendorUsageLoading(true);

    try {
      const rows = await api.vendors.models(vendor.id);
      setVendorUsageRows(rows);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.vendorManagement.vendorUsageLoadFailed);
      setVendorUsageRows([]);
    } finally {
      setVendorUsageLoading(false);
    }
  };

  const handleDeleteVendor = async () => {
    const messages = getStaticMessages();

    if (!deleteVendorConfirm) {
      return;
    }

    setVendorDeleting(true);
    try {
      await api.vendors.delete(deleteVendorConfirm.id);
      commitVendors((current) => current.filter((vendor) => vendor.id !== deleteVendorConfirm.id));
      toast.success(messages.vendorManagement.vendorDeleted);
      closeDeleteVendorDialog();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.vendorManagement.vendorDeleteFailed);
    } finally {
      setVendorDeleting(false);
    }
  };

  return {
    catalogExporting,
    catalogFileInputRef,
    catalogImporting,
    catalogImportSummary,
    catalogParsedImport,
    catalogPreviewResult,
    catalogSelectedFile,
    closeDeleteVendorDialog,
    closeVendorDialog,
    deleteVendorConfirm,
    deleteVendorDialogOpen,
    deleteVendorConflict,
    displayedDeleteVendorConfirm,
    editingVendor,
    handleCatalogExport,
    handleCatalogFileSelect,
    handleCatalogImport,
    handleDeleteVendor,
    handleDeleteVendorClick,
    handleEditVendor,
    handleSaveVendor,
    openCreateVendorDialog,
    resetCatalogImportState,
    setDeleteVendorConfirm,
    setVendorDialogOpen,
    setVendorForm,
    vendorDeleting,
    vendorDialogOpen,
    vendorForm,
    vendorSaving,
    vendorUsageLoading,
    vendorUsageRows,
    vendors,
    vendorsLoading,
  };
}
