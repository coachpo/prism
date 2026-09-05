// C5 read-surface provenance: a source-linked template must show where its
// prices came from, on both the row and the append-only revision history, while
// a manually authored template shows nothing rather than a placeholder.
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TooltipProvider } from "@/components/ui/tooltip";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PricingTemplateHistoryPanel } from "@/features/pricing/PricingTemplateHistoryPanel";
import { PricingTemplatesTable } from "@/features/pricing/PricingTemplatesTable";
import type { PricingTemplate, PricingTemplateRevision } from "@/lib/types";

vi.mock("@/hooks/useTimezone", () => ({
      useTimezone: () => ({
            timezone: "UTC",
            format: (iso: string) => iso,
            loading: false,
            refresh: async () => "UTC",
      }),
}));

function sourceLinkedTemplate(
      overrides?: Partial<PricingTemplate>,
): PricingTemplate {
      return {
            id: 41,
            profile_id: 1,
            name: "openai/gpt-five-part",
            description: null,
            pricing_unit: "PER_1M",
            pricing_currency_code: "USD",
            active_currency_symbol: "$",
            catalog_provider_id: "openai",
            catalog_model_id: "gpt-five-part",
            revision_source: "catalog",
            catalog_revision: '"catalog-rev-9"',
            template_kind: "standard",
            card: {
                  input_price: "1.25",
                  output_price: "10",
                  cached_input_price: "0",
                  cache_creation_price: "1.5",
                  reasoning_price: "12.5",
            },
            version: 2,
            revision_id: 77,
            version_effective_at: null,
            reporting_currency_epoch: 1,
            revision_count: 2,
            created_at: "2026-08-25T12:00:00Z",
            updated_at: "2026-08-25T12:00:00Z",
            ...overrides,
      } as PricingTemplate;
}

function catalogRevision(
      overrides?: Partial<PricingTemplateRevision>,
): PricingTemplateRevision {
      return {
            revision_id: 77,
            version: 1,
            pricing_unit: "PER_1M",
            currency_code: "USD",
            reporting_currency_epoch: 1,
            currency_attribution: "active_epoch",
            template_kind: "standard",
            card: {
                  input_price: "1.25",
                  output_price: "10",
                  cached_input_price: "0",
                  cache_creation_price: "1.5",
                  reasoning_price: "12.5",
            },
            tier: null,
            effective_at: "2026-08-25T12:00:00Z",
            created_at: "2026-08-25T12:00:00Z",
            created_by_kind: "manual_create",
            revision_source: "catalog",
            catalog_revision: '"catalog-rev-9"',
            ...overrides,
      } as PricingTemplateRevision;
}

describe("pricing template source provenance", () => {
      it("shows the offering coordinate under a source-linked template row", () => {
            const { rerender } = render(
                  <LocaleProvider>
                        {/* 费率列的口径说明是 OperatorHelpHint，需要 TooltipProvider 才能挂载。 */}
                        <TooltipProvider>
                              <PricingTemplatesTable
                                    detailHistory={[]}
                                    detailHistoryError={null}
                                    detailHistoryLoading={false}
                                    detailUsage={[]}
                                    detailUsageError={null}
                                    detailUsageLoading={false}
                                    facts={
                                          {
                                                byId: new Map(),
                                                failed: false,
                                                loading: false,
                                                refresh: vi.fn(),
                                          } as never
                                    }
                                    filter="all"
                                    onDelete={vi.fn()}
                                    onEdit={vi.fn()}
                                    onFilterChange={vi.fn()}
                                    onLoadHistory={vi.fn()}
                                    onLoadUsage={vi.fn()}
                                    onRetry={vi.fn()}
                                    pricingTemplateError={null}
                                    pricingTemplatePreparingEditId={null}
                                    pricingTemplates={[sourceLinkedTemplate()]}
                                    pricingTemplatesLoading={false}
                              />
                        </TooltipProvider>
                  </LocaleProvider>,
            );

            expect(
                  screen.getByTestId("pricing-template-source-41"),
            ).toHaveTextContent("openai/gpt-five-part");

            // A manual template carries no coordinate and must not render a placeholder.
            rerender(
                  <LocaleProvider>
                        {/* 费率列的口径说明是 OperatorHelpHint，需要 TooltipProvider 才能挂载。 */}
                        <TooltipProvider>
                              <PricingTemplatesTable
                                    detailHistory={[]}
                                    detailHistoryError={null}
                                    detailHistoryLoading={false}
                                    detailUsage={[]}
                                    detailUsageError={null}
                                    detailUsageLoading={false}
                                    facts={
                                          {
                                                byId: new Map(),
                                                failed: false,
                                                loading: false,
                                                refresh: vi.fn(),
                                          } as never
                                    }
                                    filter="all"
                                    onDelete={vi.fn()}
                                    onEdit={vi.fn()}
                                    onFilterChange={vi.fn()}
                                    onLoadHistory={vi.fn()}
                                    onLoadUsage={vi.fn()}
                                    onRetry={vi.fn()}
                                    pricingTemplateError={null}
                                    pricingTemplatePreparingEditId={null}
                                    pricingTemplates={[
                                          sourceLinkedTemplate({
                                                id: 42,
                                                name: "Manual",
                                                catalog_provider_id: null,
                                                catalog_model_id: null,
                                                revision_source: "manual",
                                                catalog_revision: null,
                                          }),
                                    ]}
                                    pricingTemplatesLoading={false}
                              />
                        </TooltipProvider>
                  </LocaleProvider>,
            );
            expect(
                  screen.queryByTestId("pricing-template-source-42"),
            ).not.toBeInTheDocument();
      });

      it("labels each revision's source and shows the catalog revision it replayed against", () => {
            render(
                  <LocaleProvider>
                        <PricingTemplateHistoryPanel
                              error={null}
                              loading={false}
                              onRetry={vi.fn()}
                              revisions={[
                                    catalogRevision(),
                                    catalogRevision({
                                          revision_id: 78,
                                          version: 2,
                                          revision_source: "manual",
                                          catalog_revision: null,
                                    }),
                              ]}
                              template={sourceLinkedTemplate()}
                        />
                  </LocaleProvider>,
            );

            // The source label and its value render in one element, so match the pair.
            expect(screen.getByText(/修订来源: 目录导入/)).toBeInTheDocument();
            expect(screen.getByText(/修订来源: 人工编写/)).toBeInTheDocument();
            expect(
                  screen.getByText(/目录修订: "catalog-rev-9"/),
            ).toBeInTheDocument();
      });

      it("names an unrecognised revision source instead of leaking the enum key", () => {
            render(
                  <LocaleProvider>
                        <PricingTemplateHistoryPanel
                              error={null}
                              loading={false}
                              onRetry={vi.fn()}
                              revisions={[
                                    catalogRevision({
                                          revision_source:
                                                "future_kind" as never,
                                    }),
                              ]}
                              template={sourceLinkedTemplate()}
                        />
                  </LocaleProvider>,
            );
            expect(
                  screen.getByText(/未识别的修订来源：future_kind/),
            ).toBeInTheDocument();
      });
});
