import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { BackupSection } from "../sections/BackupSection";
import { BillingCurrencySection } from "../sections/BillingCurrencySection";
import { RetentionDeletionSection } from "../sections/RetentionDeletionSection";
import { TimezoneSection } from "../sections/TimezoneSection";

describe("settings sections content i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized backup copy", () => {
    render(
      <LocaleProvider>
        <BackupSection
          selectedProfileLabel="Default (#1)"
          exporting={false}
          handleExport={vi.fn()}
          fileInputRef={{ current: null }}
          handleFileSelect={vi.fn()}
          selectedFile={null}
          parsedConfig={null}
          previewResult={null}
          importSummary={{ endpointsCount: 0, strategiesCount: 0, modelsCount: 0, connectionsCount: 0 }}
          importing={false}
          handleImport={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("备份")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "导出配置" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "导入配置" })).toBeInTheDocument();
  });

  it("renders localized billing and timezone copy", () => {
    render(
      <LocaleProvider>
        <>
          <BillingCurrencySection
            billingDirty={false}
            renderSectionSaveState={() => null}
            handleSaveCostingSettings={vi.fn()}
            costingUnavailable={false}
            costingLoading={false}
            costingSaving={false}
            costingForm={{
              report_currency_code: "USD",
              report_currency_symbol: "$",
              timezone_preference: null,
              endpoint_fx_mappings: [],
            }}
            setCostingForm={vi.fn()}
            normalizedCurrentCosting={{
              report_currency_code: "USD",
              report_currency_symbol: "$",
              timezone_preference: null,
              endpoint_fx_mappings: [],
            }}
            nativeModels={[]}
            modelLabelMap={new Map()}
            mappingModelId=""
            setMappingModelId={vi.fn()}
            loadMappingConnections={vi.fn()}
            mappingEndpointId=""
            setMappingEndpointId={vi.fn()}
            mappingConnections={[]}
            mappingLoading={false}
            mappingEndpointOptions={[]}
            mappingFxRate=""
            setMappingFxRate={vi.fn()}
            addMappingFxError={null}
            handleAddFxMapping={vi.fn()}
            editingMappingKey={null}
            editingMappingFxRate=""
            setEditingMappingFxRate={vi.fn()}
            editMappingFxError={null}
            handleSaveEditFxMapping={vi.fn()}
            handleCancelEditFxMapping={vi.fn()}
            handleStartEditFxMapping={vi.fn()}
            handleDeleteFxMapping={vi.fn()}
          />
          <TimezoneSection
            timezoneDirty={false}
            renderSectionSaveState={() => null}
            handleSaveCostingSettings={vi.fn()}
            costingUnavailable={false}
            costingLoading={false}
            costingSaving={false}
            costingForm={{
              report_currency_code: "USD",
              report_currency_symbol: "$",
              timezone_preference: null,
              endpoint_fx_mappings: [],
            }}
            setCostingForm={vi.fn()}
            timezonePreviewText="2026-02-27 23:39"
            timezonePreviewZone="Europe/Helsinki"
          />
        </>
      </LocaleProvider>,
    );

    expect(screen.getByText("计费与货币")).toBeInTheDocument();
    expect(screen.getByText("报告货币")).toBeInTheDocument();
    expect(screen.getByText("FX 映射")).toBeInTheDocument();
    expect(screen.getByText("时区")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存时区" })).toBeInTheDocument();
  });

  it("renders localized retention copy", () => {
    render(
      <LocaleProvider>
        <RetentionDeletionSection
          selectedProfileLabel="Default (#1)"
          cleanupType=""
          setCleanupType={vi.fn()}
          retentionPreset=""
          setRetentionPreset={vi.fn()}
          deleting={false}
          handleOpenDeleteConfirm={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("保留与删除")).toBeInTheDocument();
    expect(screen.getByText("数据类型")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除数据" })).toBeInTheDocument();
  });
});
