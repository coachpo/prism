import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getSharedVendors, setSharedVendors } from "@/lib/referenceData";
import type { Vendor, VendorModelUsageItem } from "@/lib/types";
import { toast } from "sonner";
import {
  DEFAULT_VENDOR_FORM,
  normalizeVendorPayload,
  parseVendorUsageRows,
  vendorFormStateFromVendor,
  type VendorFormState,
} from "./vendorManagementFormState";

interface UseVendorManagementDataInput {
  revision: number;
}

export function useVendorManagementData({ revision }: UseVendorManagementDataInput) {
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

  const closeVendorDialog = () => {
    setVendorDialogOpen(false);
  };

  const closeDeleteVendorDialog = () => {
    setDeleteVendorDialogOpen(false);
    setDeleteVendorConfirm(null);
  };

  const openCreateVendorDialog = () => {
    setEditingVendor(null);
    setVendorForm(DEFAULT_VENDOR_FORM);
    setVendorDialogOpen(true);
  };

  const handleEditVendor = (vendor: Vendor) => {
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
      if (error instanceof ApiError && error.status === 409) {
        const conflictRows = parseVendorUsageRows(error.detail);
        setDeleteVendorConflict(conflictRows);
        toast.error(messages.vendorManagement.vendorInUseDeleteBlocked);
      } else {
        toast.error(error instanceof Error ? error.message : messages.vendorManagement.vendorDeleteFailed);
      }
    } finally {
      setVendorDeleting(false);
    }
  };

  return {
    closeDeleteVendorDialog,
    closeVendorDialog,
    deleteVendorConfirm,
    deleteVendorDialogOpen,
    deleteVendorConflict,
    displayedDeleteVendorConfirm,
    editingVendor,
    handleDeleteVendor,
    handleDeleteVendorClick,
    handleEditVendor,
    handleSaveVendor,
    openCreateVendorDialog,
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
