import { Database } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { FieldGroup, FieldLegend, FieldSet } from "@/components/ui/field";
import type { BootstrapConfigResponse, BootstrapConfigSecretKey, BootstrapConfigValues } from "@/lib/types";
import {
  DATABASE_FIELD_PATHS,
  POSTGRES_POOL_LANES,
  getPostgresPoolLaneLabel,
  numberValue,
  type FieldErrors,
  type SecretInputState,
  type SettingsStartupCopy,
} from "./startupFieldMetadata";
import {
  SecretReplacementField,
  StartupInputField,
  type FieldEffectRenderer,
  type SectionEffectRenderer,
} from "./StartupServerSection";

interface StartupDatabaseSectionProps {
  bootstrapConfig: BootstrapConfigResponse;
  clearSecretInput: (secretKey: BootstrapConfigSecretKey) => void;
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  fieldErrors: FieldErrors;
  fieldEffect: FieldEffectRenderer;
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
  fieldEffect,
  handleSecretInputChange,
  sectionEffect,
  secretInputs,
  setNumberField,
  values,
}: StartupDatabaseSectionProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2 text-sm">
          <Database />
          {copy.databaseAndCapacityTitle}
          {sectionEffect(DATABASE_FIELD_PATHS)}
        </CardTitle>
        <CardDescription>{copy.databaseAndCapacityDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldSet disabled={controlsDisabled}>
          <FieldLegend>{copy.database}</FieldLegend>
          <FieldGroup>
            <SecretReplacementField
              id="startup-database-url"
              label={copy.databaseUrl}
              effect={fieldEffect("database.url")}
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
              effect={fieldEffect("database.pools.total_max_conns")}
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
                      effect={fieldEffect(`database.pools.${lane}.max_conns`)}
                      type="number"
                      value={numberValue(values.database.pools[lane].max_conns)}
                      error={fieldErrors[`database.pools.${lane}.max_conns`]}
                      disabled={controlsDisabled}
                      onChange={(value) => setNumberField(`database.pools.${lane}.max_conns`, value)}
                    />
                    <StartupInputField
                      id={`startup-${lane}-min-idle`}
                      label={copy.postgresLaneMinIdle(label)}
                      effect={fieldEffect(`database.pools.${lane}.min_idle_conns`)}
                      type="number"
                      value={numberValue(values.database.pools[lane].min_idle_conns)}
                      error={fieldErrors[`database.pools.${lane}.min_idle_conns`]}
                      disabled={controlsDisabled}
                      onChange={(value) => setNumberField(`database.pools.${lane}.min_idle_conns`, value)}
                    />
                  </div>
                );
              })}
              <StartupInputField
                id="startup-m2-concurrent"
                label={copy.m2MaxConcurrent}
                effect={fieldEffect("database.management_admission.m2_max_concurrent")}
                type="number"
                value={numberValue(values.database.management_admission.m2_max_concurrent)}
                error={fieldErrors["database.management_admission.m2_max_concurrent"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("database.management_admission.m2_max_concurrent", value)}
              />
              <StartupInputField
                id="startup-m3-concurrent"
                label={copy.m3MaxConcurrent}
                effect={fieldEffect("database.management_admission.m3_max_concurrent")}
                type="number"
                value={numberValue(values.database.management_admission.m3_max_concurrent)}
                error={fieldErrors["database.management_admission.m3_max_concurrent"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("database.management_admission.m3_max_concurrent", value)}
              />
            </div>
          </FieldGroup>
        </FieldSet>
      </CardContent>
    </Card>
  );
}
