import {
  OperatorInsetPanel,
  OperatorMissingValue,
} from "@/shared/design-system";
import type { PiCandidateWire } from "./exportTypes";
import { PiDroppedFieldsEvidence } from "./PiDroppedFieldsEvidence";

type Copy = Record<string, string>;

export function PiCandidateEvidence({
  candidate,
  copy,
}: {
  candidate: PiCandidateWire | null | undefined;
  copy: Copy;
}) {
  if (!candidate) return null;
  const rows = [
    [copy.overrideNameLabel, candidate.name],
    [
      copy.overrideReasoningLabel,
      candidate.reasoning === undefined
        ? undefined
        : candidate.reasoning
          ? copy.overrideBooleanTrue
          : copy.overrideBooleanFalse,
    ],
    [
      copy.overrideInputLabel,
      candidate.input === undefined
        ? undefined
        : candidate.input.length > 0
          ? candidate.input.join(", ")
          : "[]",
    ],
    [copy.overrideContextWindowLabel, candidate.context_window],
    [copy.overrideMaxTokensLabel, candidate.max_tokens],
    [
      copy.overrideThinkingLevelMapLabel,
      candidate.thinking_level_map === undefined
        ? undefined
        : JSON.stringify(candidate.thinking_level_map),
    ],
    [
      copy.overrideCompatLabel,
      candidate.compat === undefined
        ? undefined
        : JSON.stringify(candidate.compat),
    ],
  ];

  return (
    <OperatorInsetPanel title={copy.candidateEvidenceTitle}>
      <dl className="grid gap-x-3 gap-y-1 text-xs sm:grid-cols-[max-content_minmax(0,1fr)]">
        {rows.map(([label, value]) => (
          <div key={String(label)} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-all font-mono">
              {value === undefined ? (
                <OperatorMissingValue reason={copy.candidateFieldAbsent} />
              ) : (
                String(value)
              )}
            </dd>
          </div>
        ))}
      </dl>
      <PiDroppedFieldsEvidence
        fields={candidate.dropped_fields}
        label={copy.droppedFieldsLabel}
      />
    </OperatorInsetPanel>
  );
}
