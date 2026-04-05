import type { RefObject } from "react";
import { Shield } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { Vendor } from "@/lib/types";
import { AuditConfigurationVendorToggles } from "./AuditConfigurationVendorToggles";

interface AuditConfigurationDefaultsCardProps {
  cardRef?: RefObject<HTMLDivElement | null>;
  className?: string;
  vendors: Vendor[];
  toggleAudit: (vendorId: number, checked: boolean) => Promise<void>;
  toggleBodies: (vendorId: number, checked: boolean) => Promise<void>;
}

export function AuditConfigurationDefaultsCard({
  cardRef,
  className,
  vendors,
  toggleAudit,
  toggleBodies,
}: AuditConfigurationDefaultsCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAudit;

  return (
    <Card ref={cardRef} tabIndex={-1} className={className}>
      <CardHeader className="pb-3">
        <div className="space-y-1">
          <CardTitle className="flex items-center gap-2 text-sm">
            <Shield className="h-4 w-4" />
            {copy.auditAndPrivacy}
          </CardTitle>
          <CardDescription className="text-xs">
            {copy.captureAndPrivacyDefaults}
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="rounded-md border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
          <p>
            <span className="font-medium text-foreground">{copy.audit}:</span> {copy.recordMetadata}
          </p>
          <p>
            <span className="font-medium text-foreground">{copy.bodies}:</span> {copy.bodiesSensitive}
          </p>
        </div>

        {vendors.length === 0 ? (
          <div className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">
            {copy.noVendorsAvailable}
          </div>
        ) : (
          <AuditConfigurationVendorToggles
            vendors={vendors}
            toggleAudit={toggleAudit}
            toggleBodies={toggleBodies}
          />
        )}
      </CardContent>
    </Card>
  );
}
