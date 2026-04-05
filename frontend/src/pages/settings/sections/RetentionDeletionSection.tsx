import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Label } from "@/components/ui/label";
import {
  type CleanupType,
  type RetentionPreset,
} from "../settingsPageHelpers";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface RetentionDeletionSectionProps {
  selectedProfileLabel: string;
  cleanupType: CleanupType;
  setCleanupType: (type: CleanupType) => void;
  retentionPreset: RetentionPreset;
  setRetentionPreset: (preset: RetentionPreset) => void;
  deleting: boolean;
  handleOpenDeleteConfirm: () => void;
}

export function RetentionDeletionSection({
  selectedProfileLabel,
  cleanupType,
  setCleanupType,
  retentionPreset,
  setRetentionPreset,
  deleting,
  handleOpenDeleteConfirm,
}: RetentionDeletionSectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsRetentionDeletion;
  const dialogCopy = messages.settingsDialogs;
  return (
    <section id="retention-deletion" tabIndex={-1} className="scroll-mt-24">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-sm">
            <Trash2 className="h-4 w-4" />
            {copy.title}
          </CardTitle>
          <CardDescription className="text-xs">
            {copy.description(selectedProfileLabel)}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-2">
              <Label>{copy.dataType}</Label>
              <Select
                value={cleanupType}
                onValueChange={(value) =>
                  setCleanupType(value as CleanupType)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder={copy.selectDataType} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="requests">{dialogCopy.cleanupTypeRequests}</SelectItem>
                  <SelectItem value="statistics">{dialogCopy.cleanupTypeStatistics}</SelectItem>
                  <SelectItem value="audits">{dialogCopy.cleanupTypeAudits}</SelectItem>
                  <SelectItem value="loadbalance_events">{dialogCopy.cleanupTypeLoadbalanceEvents}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>{copy.deleteOlderThan}</Label>
              <Select
                value={retentionPreset}
                onValueChange={(value) =>
                  setRetentionPreset(value as RetentionPreset)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder={copy.selectRetention} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">{copy.retentionDays(1)}</SelectItem>
                  <SelectItem value="7">{copy.retentionDays(7)}</SelectItem>
                  <SelectItem value="30">{copy.retentionDays(30)}</SelectItem>
                  <SelectItem value="90">{copy.retentionDays(90)}</SelectItem>
                  <SelectItem value="all" className="text-destructive">
                    {copy.allData}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-end">
              <Button
                type="button"
                variant="destructive"
                className="w-full"
                disabled={deleting || !cleanupType || !retentionPreset}
                onClick={handleOpenDeleteConfirm}
              >
                {copy.deleteData}
              </Button>
            </div>
          </div>

          <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {copy.dangerDescription(selectedProfileLabel)}
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
