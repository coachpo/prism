import { useMemo, useState } from "react";
import { Plus, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";

interface UsageModelLineSelectorSectionProps {
  availableModelLineIds: string[];
  onSetSelectedModelLines: (modelIds: string[]) => void;
  selectedModelLineIds: string[];
}

const MAX_VISIBLE_LINES = 9;

export function UsageModelLineSelectorSection({
  availableModelLineIds,
  onSetSelectedModelLines,
  selectedModelLineIds,
}: UsageModelLineSelectorSectionProps) {
  const { messages } = useLocale();
  const addableModelIds = useMemo(
    () => availableModelLineIds.filter((modelId) => !selectedModelLineIds.includes(modelId)),
    [availableModelLineIds, selectedModelLineIds],
  );
  const [pendingModelId, setPendingModelId] = useState<string>(addableModelIds[0] ?? "");
  const resolvedPendingModelId = addableModelIds.includes(pendingModelId)
    ? pendingModelId
    : (addableModelIds[0] ?? "");

  const handleAddLine = () => {
    if (!resolvedPendingModelId || selectedModelLineIds.length >= MAX_VISIBLE_LINES) {
      return;
    }

    onSetSelectedModelLines([...selectedModelLineIds, resolvedPendingModelId]);
  };

  const handleRemoveLine = (modelId: string) => {
    onSetSelectedModelLines(selectedModelLineIds.filter((value) => value !== modelId));
  };

  return (
    <section className="space-y-4" data-testid="usage-model-line-section">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold tracking-tight">{messages.statistics.linesToDisplay}</h2>
      </div>

      <Card className="overflow-hidden border-border/70 bg-card/95 shadow-none">
        <CardContent className="space-y-5 p-[var(--density-card-pad-x)]">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge className="rounded-full px-3" variant="outline">
                  {messages.statistics.linesSelected(String(selectedModelLineIds.length), String(MAX_VISIBLE_LINES))}
                </Badge>
                {selectedModelLineIds.length >= MAX_VISIBLE_LINES ? (
                  <span className="text-sm text-muted-foreground">{messages.statistics.lineLimitReached}</span>
                ) : null}
              </div>

              <div className="flex flex-wrap gap-2">
                {selectedModelLineIds.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{messages.statistics.noDataAvailable}</p>
                ) : (
                  selectedModelLineIds.map((modelId) => (
                    <Badge className="gap-1 px-2.5 py-1.5" key={modelId} variant="secondary">
                      {modelId}
                      <button
                        aria-label={messages.statistics.removeLine(modelId)}
                        className="rounded-full p-0.5 hover:bg-foreground/10"
                        onClick={() => handleRemoveLine(modelId)}
                        type="button"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </Badge>
                  ))
                )}
              </div>
            </div>

            <div className="flex w-full flex-col gap-2 sm:flex-row sm:items-center xl:w-auto">
              <Select onValueChange={setPendingModelId} value={resolvedPendingModelId}>
                <SelectTrigger className="w-full min-w-56 sm:w-72">
                  <SelectValue placeholder={messages.statistics.selectModelLinePlaceholder} />
                </SelectTrigger>
                <SelectContent>
                  {addableModelIds.map((modelId) => (
                    <SelectItem key={modelId} value={modelId}>
                      {modelId}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Button
                disabled={!resolvedPendingModelId || selectedModelLineIds.length >= MAX_VISIBLE_LINES}
                onClick={handleAddLine}
                size="sm"
              >
                <Plus className="mr-2 h-4 w-4" />
                {messages.statistics.addLine}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
