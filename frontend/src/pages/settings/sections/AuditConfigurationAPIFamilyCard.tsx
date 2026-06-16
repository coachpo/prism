import type { ReactNode, RefObject } from "react";
import { ShieldCheck } from "lucide-react";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily, AuditAPIFamilySetting } from "@/lib/types";
import { OperatorLoadingState, OperatorSectionCard } from "@/shared/design-system";

interface AuditConfigurationAPIFamilyCardProps {
  apiFamilyAuditSettings: AuditAPIFamilySetting[];
  apiFamilyAuditSettingsDirty: boolean;
  cardRef: RefObject<HTMLDivElement | null>;
  className?: string;
  loadingAPIFamilyAuditSettings: boolean;
  renderSectionSaveState: (section: "audit", isDirty: boolean) => ReactNode;
  savingAPIFamilyAuditSettings: boolean;
  handleSaveAPIFamilyAuditSettings: () => Promise<void>;
  setAPIFamilyAuditCaptureBodies: (apiFamily: ApiFamily, checked: boolean) => void;
  setAPIFamilyAuditEnabled: (apiFamily: ApiFamily, checked: boolean) => void;
}

export function AuditConfigurationAPIFamilyCard({
  apiFamilyAuditSettings,
  apiFamilyAuditSettingsDirty,
  cardRef,
  className,
  loadingAPIFamilyAuditSettings,
  renderSectionSaveState,
  savingAPIFamilyAuditSettings,
  handleSaveAPIFamilyAuditSettings,
  setAPIFamilyAuditCaptureBodies,
  setAPIFamilyAuditEnabled,
}: AuditConfigurationAPIFamilyCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAudit;

  return (
    <OperatorSectionCard
      ref={cardRef}
      className={className}
      data-testid="audit-api-family-card"
      title={(
        <span className="flex items-center gap-2">
          <ShieldCheck data-icon="inline-start" />
          {copy.apiFamilyAuditControls}
        </span>
      )}
      description={copy.apiFamilyAuditDescription}
      actions={(
        <div className="flex items-center gap-2">
          {renderSectionSaveState("audit", apiFamilyAuditSettingsDirty)}
          <Button
            type="button"
            size="sm"
            disabled={
              loadingAPIFamilyAuditSettings ||
              savingAPIFamilyAuditSettings ||
              !apiFamilyAuditSettingsDirty
            }
            onClick={() => void handleSaveAPIFamilyAuditSettings()}
          >
            {savingAPIFamilyAuditSettings ? copy.savingAuditSettings : copy.saveAuditSettings}
          </Button>
        </div>
      )}
    >
      {loadingAPIFamilyAuditSettings ? (
        <OperatorLoadingState title={copy.loadingAPIFamilyAuditSettings} />
      ) : (
        <div className="operator-table-shell overflow-hidden rounded-md border border-outline-variant">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{copy.apiFamily}</TableHead>
                <TableHead className="w-[140px]">{copy.auditEnabled}</TableHead>
                <TableHead className="w-[140px]">{copy.captureBodies}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {apiFamilyAuditSettings.map((setting) => {
                const familyLabel = getAPIFamilyLabel(setting.api_family, copy);
                return (
                  <TableRow key={setting.api_family} data-testid={`audit-api-family-row-${setting.api_family}`}>
                    <TableCell>
                      <div className="flex items-center gap-2 font-medium">
                        <ApiFamilyIcon apiFamily={setting.api_family} />
                        {familyLabel}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Switch
                        aria-label={`${familyLabel} ${copy.auditEnabled}`}
                        checked={setting.audit_enabled}
                        disabled={savingAPIFamilyAuditSettings}
                        onCheckedChange={(checked) =>
                          setAPIFamilyAuditEnabled(setting.api_family, checked)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Switch
                        aria-label={`${familyLabel} ${copy.captureBodies}`}
                        checked={setting.audit_capture_bodies}
                        disabled={!setting.audit_enabled || savingAPIFamilyAuditSettings}
                        onCheckedChange={(checked) =>
                          setAPIFamilyAuditCaptureBodies(setting.api_family, checked)
                        }
                      />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </OperatorSectionCard>
  );
}

function getAPIFamilyLabel(apiFamily: ApiFamily, copy: ReturnType<typeof useLocale>["messages"]["settingsAudit"]) {
  switch (apiFamily) {
    case "openai":
      return copy.openaiFamily;
    case "anthropic":
      return copy.anthropicFamily;
    case "gemini":
      return copy.geminiFamily;
  }
}
