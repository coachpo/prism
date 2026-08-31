import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuGroup,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { MoreHorizontal } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import {
    OperatorCallout,
    OperatorDestructiveDialog,
    OperatorStatusBadge,
    OperatorTypeBadge,
} from "@/shared/design-system";
import type { ExportSourceModelRow } from "@/lib/types";
import { PiBindingOverrideDialog } from "@/features/models/catalog/pi/PiBindingOverrideDialog";
import { PiBindingRefreshDialog } from "@/features/models/catalog/pi/PiBindingRefreshDialog";
import { PiBindingSourceDialog } from "@/features/models/catalog/pi/PiBindingSourceDialog";
import { PiDroppedFieldsEvidence } from "@/features/models/catalog/pi/PiDroppedFieldsEvidence";
import type { PiBindingController } from "@/features/models/catalog/pi/usePiBindingController";
import {
    isModelExportSourceReconciliationError,
    type ModelExportSourceState,
} from "./useModelExportSource";

type Copy = Record<string, string>;

const BINDING_STATUS_LABEL_KEYS: Record<string, string> = {
    bound: "bindingStatusBound",
    bound_drifted: "bindingStatusDrifted",
    unbound: "bindingStatusUnbound",
};

const CANDIDATE_STATUS_LABEL_KEYS: Record<string, string> = {
    not_in_catalog: "candidateStatusNotInCatalog",
    api_mismatch: "candidateStatusApiMismatch",
    single: "candidateStatusSingle",
    multiple: "candidateStatusMultiple",
    catalog_unavailable: "candidateStatusCatalogUnavailable",
};

function apiErrorDetail(error: unknown, copy: Copy): string {
    if (isModelExportSourceReconciliationError(error)) {
        return copy.sourceReconciliationFailed;
    }
    return error instanceof Error ? error.message : String(error);
}

export function PiBindingCell({
    model,
    sourceState,
    controller,
}: {
    model: ExportSourceModelRow;
    sourceState: ModelExportSourceState;
    controller: PiBindingController;
}) {
    const { messages } = useLocale();
    const copy = messages.modelExportPage as Copy;
    const [sourceOpen, setSourceOpen] = useState(false);
    const [refreshOpen, setRefreshOpen] = useState(false);
    const [overrideOpen, setOverrideOpen] = useState(false);
    const [unbindOpen, setUnbindOpen] = useState(false);
    const [actionError, setActionError] = useState<string | null>(null);
    const selected = model.pi_selected;
    const candidateKey = CANDIDATE_STATUS_LABEL_KEYS[model.candidate_status];
    // A directory entry exists for every model whose final Pi API is
    // determinable, including the ones the default exact search reports as
    // not_in_catalog or api_mismatch. Only a model with no Pi text API at all
    // has nothing to bind to.
    const canBind = Boolean(model.pi_api);

    if (!selected) {
        return (
            <div className="flex flex-col gap-1">
                <div className="flex flex-wrap items-center gap-1">
                    <OperatorTypeBadge
                        intent="muted"
                        label={copy[candidateKey] ?? copy.candidateStatusUnknown}
                        title={candidateKey ? undefined : model.candidate_status}
                    />
                    <OperatorTypeBadge
                        intent="muted"
                        label={copy.bindingStatusUnbound}
                    />
                </div>
                {canBind ? (
                    <div className="flex flex-wrap gap-1 opacity-70 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={sourceState.sourceActionsBlocked}
                            onClick={() => setSourceOpen(true)}
                        >
                            {copy.bindSourceAction}
                        </Button>
                    </div>
                ) : (
                    <p className="text-xs text-muted-foreground">
                        {copy.noPiApiCannotBind}
                    </p>
                )}
                {sourceOpen ? (
                    <PiBindingSourceDialog
                        copy={copy}
                        view={sourceState.piViewFor(model)}
                        onClose={() => setSourceOpen(false)}
                        controller={controller}
                    />
                ) : null}
            </div>
        );
    }

    const bindingKey = BINDING_STATUS_LABEL_KEYS[model.pi_binding_status];
    const boundPrismModelId = model.pi_binding_prism_model_id;
    const isCrossDirectory = boundPrismModelId
        ? selected.model_id !== boundPrismModelId
        : false;

    async function handleUnbind() {
        setActionError(null);
        try {
            await controller.unbind(model.model_config_id);
            setUnbindOpen(false);
        } catch (error) {
            setActionError(apiErrorDetail(error, copy));
        }
    }

    return (
        <div className="flex flex-col gap-1">
            <span className="font-mono text-xs">
                {selected.provider_id}/{selected.model_id} ({selected.api})
            </span>
            <p className="text-xs text-muted-foreground">
                {copy.boundIdentityLabel}:{" "}
                <span className="font-mono">
                    {boundPrismModelId ?? copy.bindingIdentityAbsent}
                </span>
                {isCrossDirectory
                    ? ` · ${copy.boundCrossDirectoryLabel}`
                    : null}
            </p>
            <div className="flex flex-wrap items-center gap-1">
                <OperatorTypeBadge
                    intent="muted"
                    label={copy[candidateKey] ?? copy.candidateStatusUnknown}
                    title={candidateKey ? undefined : model.candidate_status}
                />
                <OperatorStatusBadge
                    intent={
                        model.pi_binding_status === "bound"
                            ? "healthy"
                            : "degraded"
                    }
                    label={copy[bindingKey] ?? copy.bindingStatusUnknown}
                    title={bindingKey ? undefined : model.pi_binding_status}
                />
            </div>
            {!model.pi_binding_renderable ? (
                <p className="text-xs text-destructive">
                    {copy.bindingNotRenderable}
                </p>
            ) : null}
            {!boundPrismModelId ? (
                <OperatorCallout
                    intent="danger"
                    description={copy.bindingIdentityIntegrityError}
                />
            ) : null}
            {!canBind ? (
                <p className="text-xs text-muted-foreground">
                    {copy.noPiApiCannotBind}
                </p>
            ) : null}
            <PiDroppedFieldsEvidence
                fields={model.pi_binding_dropped_fields}
                label={copy.droppedFieldsLabel}
            />
            <div className="flex flex-wrap gap-1 opacity-70 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button
                            type="button"
                            size="sm"
                            variant="ghost"
                            aria-label={copy.bindingActionsLabel}
                            title={copy.bindingActionsLabel}
                            disabled={controller.actionsBlocked}
                        >
                            <MoreHorizontal data-icon="inline-start" />
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start">
                        <DropdownMenuGroup>
                            <DropdownMenuItem
                                disabled={!canBind || controller.actionsBlocked}
                                onSelect={() => setSourceOpen(true)}
                            >
                                {copy.changeSourceAction}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                                disabled={
                                    !model.pi_binding_renderable ||
                                    controller.actionsBlocked
                                }
                                onSelect={() => setRefreshOpen(true)}
                            >
                                {copy.refreshAction}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                                disabled={
                                    !model.pi_binding_renderable ||
                                    controller.actionsBlocked
                                }
                                onSelect={() => setOverrideOpen(true)}
                            >
                                {copy.overrideAction}
                            </DropdownMenuItem>
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator />
                        <DropdownMenuGroup>
                            <DropdownMenuItem
                                variant="destructive"
                                onSelect={() => {
                                    setActionError(null);
                                    setUnbindOpen(true);
                                }}
                            >
                                {copy.unbindAction}
                            </DropdownMenuItem>
                        </DropdownMenuGroup>
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>
            {refreshOpen && model.pi_binding_renderable ? (
                <PiBindingRefreshDialog
                    copy={copy}
                    modelConfigId={model.model_config_id}
                    onClose={() => setRefreshOpen(false)}
                    controller={controller}
                />
            ) : null}
            {sourceOpen ? (
                <PiBindingSourceDialog
                    copy={copy}
                    view={sourceState.piViewFor(model)}
                    onClose={() => setSourceOpen(false)}
                    controller={controller}
                />
            ) : null}
            {overrideOpen && model.pi_binding_renderable ? (
                <PiBindingOverrideDialog
                    copy={copy}
                    view={sourceState.piViewFor(model)}
                    onClose={() => setOverrideOpen(false)}
                    controller={controller}
                />
            ) : null}
            {unbindOpen ? (
                <OperatorDestructiveDialog
                    open
                    onOpenChange={(open) => {
                        if (!controller.mutationPending) setUnbindOpen(open);
                    }}
                    title={copy.unbindConfirmTitle}
                    description={copy.unbindConfirmDescription}
                    cancelLabel={copy.cancel}
                    confirmLabel={copy.unbindConfirm}
                    confirmingLabel={copy.unbinding}
                    confirming={controller.mutationPending}
                    confirmDisabled={controller.actionsBlocked}
                    cancelDisabled={controller.mutationPending}
                    confirmTestId="pi-unbind-confirm"
                    onCancel={() => setUnbindOpen(false)}
                    onConfirm={handleUnbind}
                >
                    {model.pi_binding_override ? (
                        <OperatorCallout
                            intent="warning"
                            description={copy.unbindOverridesWarning}
                        />
                    ) : null}
                    {actionError ? (
                        <OperatorCallout
                            intent="danger"
                            description={actionError}
                        />
                    ) : null}
                </OperatorDestructiveDialog>
            ) : null}
        </div>
    );
}
