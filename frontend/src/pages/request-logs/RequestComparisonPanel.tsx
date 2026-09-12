import { useState, useSyncExternalStore } from "react";
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance";
import type { RequestLogListItem } from "@/lib/types";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel, FieldGroup } from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import { OperatorCallout } from "@/shared/design-system";
import { RequestComparisonSide } from "./RequestComparisonSide";
import { AuditCaptureLedger } from "./AuditCaptureLedger";
import {
  comparisonBody,
  compareRetainedLines,
  type ComparisonDirection,
} from "./requestComparison";

import { useRequestComparisonCapture } from "./useRequestComparisonCapture";

function ComparisonSession({ items }: { items: RequestLogListItem[] }) {
  const { messages } = useLocale();
  const copy = messages.requestComparison;
  const [ids, setIds] = useState<[string, string]>(["", ""]);
  const [direction, setDirection] = useState<ComparisonDirection>("response");
  const { captures, cancel, load } = useRequestComparisonCapture(ids);
  const [choices, setChoices] = useState<RequestLogListItem[]>([]);
  const candidates = [
    ...new Map(
      [...choices, ...items].map((item) => [item.request_log_id, item]),
    ).values(),
  ];
  const bodies = captures.map((capture, index) =>
    capture.detail ? comparisonBody(capture.detail, direction, candidates.find(item => item.request_log_id === ids[index])?.api_family) : null,
  );
  const difference =
    bodies[0]?.text != null && bodies[1]?.text != null
      ? compareRetainedLines(bodies[0].text, bodies[1].text)
      : null;
  return (
    <div
      className="flex flex-col gap-3 min-w-0"
      data-testid="request-comparison"
    >
      <p className="text-xs text-muted-foreground">{copy.scope}</p>
      <Field>
        <FieldLabel>{copy.direction}</FieldLabel>
        <Select
          value={direction}
          onValueChange={(value) => setDirection(value as ComparisonDirection)}
        >
          <SelectTrigger aria-label={copy.direction}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="request">{copy.request}</SelectItem>
              <SelectItem value="response">{copy.response}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>
      <FieldGroup className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {[0, 1].map((index) => (
          <div className="flex flex-col gap-3 min-w-0" key={index}>
            <Field>
              <FieldLabel>{index === 0 ? copy.left : copy.right}</FieldLabel>
              <Select
                value={ids[index]}
                onValueChange={(value) => {
                  cancel(index);
                  setIds(
                    (current) =>
                      current.map((id, i) =>
                        index === i ? value : id,
                      ) as typeof current,
                  );
                  setChoices(
                    candidates.filter(
                      (item) =>
                        item.request_log_id === value ||
                        ids.includes(item.request_log_id),
                    ),
                  );
                }}
              >
                <SelectTrigger
                  aria-label={index === 0 ? copy.left : copy.right}
                >
                  <SelectValue placeholder={copy.select} />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {candidates.map((item) => (
                      <SelectItem
                        key={item.request_log_id}
                        value={item.request_log_id}
                        disabled={ids[1 - index] === item.request_log_id}
                      >
                        #{item.request_log_id} · {item.ingress_model_id}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            {ids[index] ? (
              <RequestComparisonSide
                key={ids[index]}
                requestId={ids[index]}
                side={index === 0 ? copy.left : copy.right}
                onLoad={(id) => void load(index, id)}
                loading={captures[index].loading}
                onCancel={() => cancel(index)}
              />
            ) : null}
            {captures[index].error ? (
              <OperatorCallout
                intent="danger"
                description={copy.unavailable}
              />
            ) : null}
            {bodies[index] ? (
              <>
                <p className="text-xs text-muted-foreground">{bodies[index].provenance === "runtime_bytes" ? copy.runtimeBytes : copy.legacyBytes} · {bodies[index].endState === "complete" ? copy.captureComplete : copy.captureIncomplete}</p>
              <AuditCaptureLedger
                  bytesObserved={bodies[index].observed}
                  bytesStored={bodies[index].stored}
                  captureStatus={bodies[index].status}
                  truncated={bodies[index].truncated}
                />
                <p className="text-xs">
                  {bodies[index].binary
                    ? copy.binary
                    : bodies[index].text === null
                      ? copy.missing
                      : bodies[index].truncated
                        ? copy.truncated
                        : copy.complete}
                </p>
                {bodies[index].text === "" ? (
                  <p>{copy.empty}</p>
                ) : bodies[index].text !== null ? (
                  <details>
                    <summary className="cursor-pointer text-xs">
                      {direction === "request" ? copy.request : copy.response}
                    </summary>
                    <p className="text-xs">{copy.diffScope}</p>
                    <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all bg-inset p-2 font-mono text-xs">
                      {bodies[index].text
                        .split("\n")
                        .slice(0, 200)
                        .map((line) => line.slice(0, 2000))
                        .join("\n")}
                    </pre>
                  </details>
                ) : null}
              </>
            ) : null}
          </div>
        ))}
      </FieldGroup>
      <OperatorCallout description={copy.retainedScope} />
      {difference ? (
        <section aria-label={copy.diff} className="min-w-0 flex flex-col gap-2">
          <h3>{difference.equal ? copy.same : copy.different}</h3>
          <p className="text-xs text-muted-foreground">{copy.diffScope}</p>
          <pre
            className="max-h-96 overflow-auto rounded-md border bg-inset p-3 font-mono text-xs whitespace-pre-wrap break-all"
            data-testid="request-comparison-diff"
          >
            {difference.lines
              .map((line) =>
                line.same
                  ? `  ${line.number} ${line.left ?? ""}`
                  : `${line.left === null ? "" : `− ${line.number} ${line.left}\n`}${line.right === null ? "" : `+ ${line.number} ${line.right}`}`,
              )
              .join("\n")}
          </pre>
        </section>
      ) : (
        <p className="text-xs text-muted-foreground">{copy.incomplete}</p>
      )}
    </div>
  );
}

export function RequestComparisonPanel({
  items,
}: {
  items: RequestLogListItem[];
}) {
  const { messages } = useLocale();
  const copy = messages.requestComparison;
  const [open, setOpen] = useState(false);
  const epoch = useSyncExternalStore(
    (listener) => authSessionCoordinator.subscribe(listener),
    () => authSessionCoordinator.getEpoch(),
  );
  return (
    <section className="operator-section-surface min-w-0 flex flex-col gap-3 p-3">
      <Button
        variant="outline"
        className="self-start"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        {open ? copy.close : copy.open}
      </Button>
      {open ? (
        <>
          <h2>{copy.title}</h2>
          <ComparisonSession key={epoch} items={items} />
        </>
      ) : null}
    </section>
  );
}
