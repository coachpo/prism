import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { OperatorSectionCard } from "@/shared/design-system";
import type { ModelExportSourceState } from "./useModelExportSource";

/** Source-backed filters and batch actions for the model selection surface. */
export function ModelExportSelectionPanel({
  sourceState,
}: {
  sourceState: ModelExportSourceState;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const {
    batchClearVisible,
    batchSelectVisible,
    familyFilter,
    metadataFilter,
    priceCompleteOnly,
    setFamilyFilter,
    setMetadataFilter,
    setPriceCompleteOnly,
    setSearchText,
    searchText,
    sourceQuery,
  } = sourceState;

  return (
    <OperatorSectionCard
      title={copy.selectionTitle}
      actions={
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void sourceQuery.refetch()}
            disabled={sourceQuery.isFetching}
          >
            {sourceQuery.isFetching ? (
              <Spinner data-icon="inline-start" />
            ) : null}
            {sourceQuery.isFetching
              ? copy.refreshingSource
              : copy.refreshSource}
          </Button>
          <Button variant="outline" size="sm" onClick={batchSelectVisible}>
            {copy.batchSelectVisible}
          </Button>
          <Button variant="outline" size="sm" onClick={batchClearVisible}>
            {copy.batchClearVisible}
          </Button>
        </div>
      }
    >
      <FieldGroup className="gap-4 md:grid md:grid-cols-2 md:items-end xl:grid-cols-4">
        <Field>
          <FieldLabel htmlFor="export-model-search">
            {copy.searchLabel}
          </FieldLabel>
          <Input
            id="export-model-search"
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            placeholder={copy.searchPlaceholder}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="export-family-filter">
            {copy.familyFilterLabel}
          </FieldLabel>
          <Select value={familyFilter} onValueChange={setFamilyFilter}>
            <SelectTrigger
              id="export-family-filter"
              aria-label={copy.familyFilterLabel}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.familyAll}</SelectItem>
                <SelectItem value="openai">OpenAI</SelectItem>
                <SelectItem value="anthropic">Anthropic</SelectItem>
                <SelectItem value="gemini">Gemini</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel htmlFor="export-metadata-filter">
            {copy.metadataFilterLabel}
          </FieldLabel>
          <Select
            value={metadataFilter}
            onValueChange={(value) =>
              setMetadataFilter(value as "all" | "complete" | "incomplete")
            }
          >
            <SelectTrigger
              id="export-metadata-filter"
              aria-label={copy.metadataFilterLabel}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.metadataAll}</SelectItem>
                <SelectItem value="complete">
                  {copy.metadataComplete}
                </SelectItem>
                <SelectItem value="incomplete">
                  {copy.metadataIncomplete}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field orientation="horizontal">
          <Switch
            id="export-price-complete"
            checked={priceCompleteOnly}
            onCheckedChange={setPriceCompleteOnly}
          />
          <FieldLabel htmlFor="export-price-complete">
            {copy.priceCompleteOnly}
          </FieldLabel>
        </Field>
      </FieldGroup>
    </OperatorSectionCard>
  );
}
