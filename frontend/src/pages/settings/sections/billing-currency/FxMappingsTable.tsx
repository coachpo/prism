import { Check, Pencil, Trash2, X } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { EndpointFxMapping } from "@/lib/types";
import { formatFxRateDisplay, getMappingKey } from "../../settingsPageHelpers";

interface FxMappingsTableProps {
  editMappingFxError: string | null;
  editingMappingFxRate: string;
  editingMappingKey: string | null;
  handleCancelEditFxMapping: () => void;
  handleDeleteFxMapping: (mapping: EndpointFxMapping) => void;
  handleSaveEditFxMapping: () => void;
  handleStartEditFxMapping: (mapping: EndpointFxMapping) => void;
  mappings: EndpointFxMapping[];
  modelLabelMap: Map<string, string>;
  setEditingMappingFxRate: (rate: string) => void;
}

export function FxMappingsTable({
  editMappingFxError,
  editingMappingFxRate,
  editingMappingKey,
  handleCancelEditFxMapping,
  handleDeleteFxMapping,
  handleSaveEditFxMapping,
  handleStartEditFxMapping,
  mappings,
  modelLabelMap,
  setEditingMappingFxRate,
}: FxMappingsTableProps) {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  if (mappings.length === 0) {
    return (
      <div className="mt-3 rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">
        {copy.endpointFxMappingsEmpty}
      </div>
    );
  }

  return (
    <div className="mt-3 rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.model}</TableHead>
            <TableHead>{copy.endpoint}</TableHead>
            <TableHead>{copy.fxRate}</TableHead>
            <TableHead>{messages.requestLogs.auditCapture}</TableHead>
            <TableHead className="text-right">{messages.pricingTemplatesUi.actions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {mappings.map((mapping) => {
            const mappingKey = getMappingKey(mapping);
            const isEditing = editingMappingKey === mappingKey;

            return (
              <TableRow key={mappingKey}>
                <TableCell className="font-medium">
                  {modelLabelMap.get(mapping.model_id) || mapping.model_id}
                </TableCell>
                <TableCell>#{mapping.endpoint_id}</TableCell>
                <TableCell>
                  {isEditing ? (
                    <div className="space-y-1">
                      <Input
                        name="editing_mapping_fx_rate"
                        autoComplete="off"
                        value={editingMappingFxRate}
                        onChange={(event) => setEditingMappingFxRate(event.target.value)}
                        className={cn("h-8 w-32", editMappingFxError && "border-destructive")}
                        inputMode="decimal"
                        aria-invalid={Boolean(editMappingFxError)}
                      />
                      {editMappingFxError ? (
                        <p className="text-xs text-destructive">{editMappingFxError}</p>
                      ) : null}
                    </div>
                  ) : (
                    formatFxRateDisplay(mapping.fx_rate)
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant="secondary">{copy.mappingSourceOverride}</Badge>
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    {isEditing ? (
                      <>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={handleSaveEditFxMapping}
                          disabled={Boolean(editMappingFxError)}
                          aria-label={copy.saveFxMapping}
                        >
                          <Check className="h-4 w-4" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={handleCancelEditFxMapping}
                          aria-label={copy.cancelFxMappingEdit}
                        >
                          <X className="h-4 w-4" />
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          onClick={() => handleStartEditFxMapping(mapping)}
                          aria-label={copy.editFxMapping}
                        >
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-destructive hover:text-destructive"
                          onClick={() => handleDeleteFxMapping(mapping)}
                          aria-label={copy.deleteFxMapping}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
