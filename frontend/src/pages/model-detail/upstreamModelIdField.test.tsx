import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ApiError } from "@/lib/api/request";
import { useModelDetailDialogState } from "./useModelDetailDialogState";
import {
  upstreamModelIdIssueFromError,
  validateUpstreamModelIdField,
} from "./upstreamModelIdField";

describe("upstream model id field contract", () => {
  it("counts Unicode code points and recognizes the typed server envelope", () => {
    for (const [value, issue] of [
      ["😀".repeat(200), null],
      ["😀".repeat(201), "too_long"],
      ["  ", "required"],
    ] as const) {
      expect(validateUpstreamModelIdField(value, true)).toBe(issue);
    }
    expect(
      upstreamModelIdIssueFromError(
        new ApiError("too long", 422, {
          detail: "upstream_model_id must be at most 200 characters",
          field: "upstream_model_id",
          reason: "too_long",
          limit: 200,
        }),
      ),
    ).toBe("too_long");
  });

  it("fills an untouched handoff draft after the owner model arrives", async () => {
    const { result, rerender } = renderHook(
      ({ ownerModelID }) =>
        useModelDetailDialogState({
          apiFamily: "openai",
          openAIMode: "dual_native",
          globalEndpoints: [],
          ownerModelID,
        }),
      { initialProps: { ownerModelID: null as string | null } },
    );

    act(() => result.current.openConnectionDialog());
    expect(result.current.connectionForm.upstream_model_id).toBeUndefined();
    rerender({ ownerModelID: "entry-late" });
    await waitFor(() =>
      expect(result.current.connectionForm.upstream_model_id).toBe(
        "entry-late",
      ),
    );

    act(() => {
      result.current.setConnectionForm({
        ...result.current.connectionForm,
        upstream_model_id: "",
      });
    });
    rerender({ ownerModelID: "entry-renamed" });
    expect(result.current.connectionForm.upstream_model_id).toBe("");
  });
});
