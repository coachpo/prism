import { Database } from "lucide-react";
import { FieldGroup, FieldSet } from "@/components/ui/field";
import { Separator } from "@/components/ui/separator";
import type { BootstrapConfigResponse, BootstrapConfigSecretKey, BootstrapConfigValues } from "@/lib/types";
import { OperatorSectionCard } from "@/shared/design-system";
import {
  DATABASE_ADMISSION_FIELD_PATHS,
  DATABASE_CORE_FIELD_PATHS,
  DATABASE_FIELD_PATHS,
  POSTGRES_POOL_LANES,
  getPostgresPoolLaneLabel,
  numberValue,
  type FieldErrors,
  type SecretInputState,
  type SettingsStartupCopy,
} from "./startupFieldMetadata";
import {
  FieldLegendWithEffect,
  SecretReplacementField,
  StartupInputField,
  type SectionEffectRenderer,
} from "./StartupServerSection";

interface StartupDatabaseSectionProps {
  bootstrapConfig: BootstrapConfigResponse;
  clearSecretInput: (secretKey: BootstrapConfigSecretKey) => void;
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  fieldErrors: FieldErrors;
  handleSecretInputChange: (secretKey: BootstrapConfigSecretKey, value: string) => void;
  sectionEffect: SectionEffectRenderer;
  secretInputs: SecretInputState;
  setNumberField: (path: string, rawValue: string) => void;
  values: BootstrapConfigValues;
}

export function StartupDatabaseSection({
  bootstrapConfig,
  clearSecretInput,
  controlsDisabled,
  copy,
  fieldErrors,
  handleSecretInputChange,
  sectionEffect,
  secretInputs,
  setNumberField,
  values,
}: StartupDatabaseSectionProps) {
  return (
    <OperatorSectionCard
      icon={<Database />}
      title={(
        <span className="flex flex-wrap items-center gap-2">
          {copy.databaseAndCapacityTitle}
          {sectionEffect(DATABASE_FIELD_PATHS)}
        </span>
      )}
      description={copy.databaseAndCapacityDescription}
      contentClassName="flex flex-col gap-6"
    >
        <FieldSet disabled={controlsDisabled}>
          <FieldLegendWithEffect label={copy.database} effect={sectionEffect(DATABASE_CORE_FIELD_PATHS)} />
          <FieldGroup>
            <SecretReplacementField
              id="startup-database-url"
              label={copy.databaseUrl}
              secretKey="database.url"
              masked={bootstrapConfig.secrets["database.url"].masked}
              configured={bootstrapConfig.secrets["database.url"].configured}
              editable={bootstrapConfig.secrets["database.url"].editable && !controlsDisabled}
              value={secretInputs["database.url"]}
              copy={copy}
              onChange={handleSecretInputChange}
              onClear={clearSecretInput}
            />
            <StartupInputField
              id="startup-postgres-total-max-conns"
              label={copy.postgresTotalMaxConns}
              type="number"
              value={numberValue(values.database.pools.total_max_conns)}
              error={fieldErrors["database.pools.total_max_conns"]}
              disabled={controlsDisabled}
              onChange={(value) => setNumberField("database.pools.total_max_conns", value)}
            />
            <div className="grid gap-4 md:grid-cols-2">
              {POSTGRES_POOL_LANES.map((lane) => {
                const label = getPostgresPoolLaneLabel(copy, lane);
                return (
                  <div key={lane} className="contents">
                    <StartupInputField
                      id={`startup-${lane}-max-conns`}
                      label={copy.postgresLaneMaxConns(label)}
                      type="number"
                      value={numberValue(values.database.pools[lane].max_conns)}
                      error={fieldErrors[`database.pools.${lane}.max_conns`]}
                      disabled={controlsDisabled}
                      onChange={(value) => setNumberField(`database.pools.${lane}.max_conns`, value)}
                    />
                    <StartupInputField
                      id={`startup-${lane}-min-idle`}
                      label={copy.postgresLaneMinIdle(label)}
                      type="number"
                      value={numberValue(values.database.pools[lane].min_idle_conns)}
                      error={fieldErrors[`database.pools.${lane}.min_idle_conns`]}
                      disabled={controlsDisabled}
                      onChange={(value) => setNumberField(`database.pools.${lane}.min_idle_conns`, value)}
                    />
                  </div>
                );
              })}
            </div>
          </FieldGroup>
        </FieldSet>
        <Separator />
        <FieldSet disabled={controlsDisabled}>
          <FieldLegendWithEffect label={copy.managementAdmission} effect={sectionEffect(DATABASE_ADMISSION_FIELD_PATHS)} />
          <FieldGroup>
            <div className="grid gap-4 md:grid-cols-2">
              <StartupInputField
                id="startup-m2-concurrent"
                label={copy.m2MaxConcurrent}
                type="number"
                value={numberValue(values.database.management_admission.m2_max_concurrent)}
                error={fieldErrors["database.management_admission.m2_max_concurrent"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("database.management_admission.m2_max_concurrent", value)}
              />
              <StartupInputField
                id="startup-m3-concurrent"
                label={copy.m3MaxConcurrent}
                type="number"
                value={numberValue(values.database.management_admission.m3_max_concurrent)}
                error={fieldErrors["database.management_admission.m3_max_concurrent"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("database.management_admission.m3_max_concurrent", value)}
              />
            </div>
          </FieldGroup>
        </FieldSet>
    </OperatorSectionCard>
  );
}
