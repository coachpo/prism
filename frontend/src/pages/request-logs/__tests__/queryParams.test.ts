import { describe, expect, it } from "vitest";
import {
  DEFAULTS,
  PAGE_SIZE_OPTIONS,
  parsePageState,
  stateToParams,
  TIME_RANGE_OPTIONS,
  timeRangeToFromTime,
} from "../queryParams";

describe("request log query params", () => {
  it("drops removed request-log filters during parse and serialization", () => {
    const parsed = parsePageState(
      new URLSearchParams(
        "ingress_request_id=ingress_req_42&model_id=gpt-5.4&endpoint_id=99&time_range=7d&status_family=5xx&request_id=1234&detail_tab=audit&search=timeout&api_family=openai&outcome_filter=error&stream_filter=yes&latency_bucket=slow&token_min=100&token_max=500&triage=true",
      ),
    );

    expect(parsed.ingress_request_id).toBe("ingress_req_42");
    expect(parsed.model_id).toBe("gpt-5.4");
    expect(parsed.endpoint_id).toBe("99");
    expect(parsed.time_range).toBe("7d");
    expect(parsed.status_family).toBe("5xx");
    expect(parsed.request_id).toBe("1234");
    expect(parsed.detail_tab).toBe("audit");
    expect("search" in parsed).toBe(false);
    expect("api_family" in parsed).toBe(false);
    expect("outcome_filter" in parsed).toBe(false);
    expect("stream_filter" in parsed).toBe(false);
    expect("latency_bucket" in parsed).toBe(false);
    expect("token_min" in parsed).toBe(false);
    expect("token_max" in parsed).toBe(false);
    expect("triage" in parsed).toBe(false);

    const params = stateToParams(parsed);
    expect(params.get("ingress_request_id")).toBe("ingress_req_42");
    expect(params.get("model_id")).toBe("gpt-5.4");
    expect(params.get("endpoint_id")).toBe("99");
    expect(params.get("status_family")).toBe("5xx");
    expect(params.get("request_id")).toBe("1234");
    expect(params.get("search")).toBeNull();
    expect(params.get("api_family")).toBeNull();
    expect(params.get("outcome_filter")).toBeNull();
    expect(params.get("stream_filter")).toBeNull();
    expect(params.get("latency_bucket")).toBeNull();
    expect(params.get("token_min")).toBeNull();
    expect(params.get("token_max")).toBeNull();
    expect(params.get("triage")).toBeNull();
  });

  it("omits default values from the URL", () => {
    const params = stateToParams(parsePageState(new URLSearchParams()));

    expect(params.toString()).toBe("");
    expect(parsePageState(new URLSearchParams()).time_range).toBe("1h");
  });

  it("keeps the approved request-log time range options and default", () => {
    expect(TIME_RANGE_OPTIONS).toEqual(["1h", "6h", "24h", "7d", "30d", "all"]);
    expect(DEFAULTS.time_range).toBe("1h");
  });

  it("enforces the request-log page-size contract", () => {
    expect(PAGE_SIZE_OPTIONS).toEqual([100, 300, 500]);
    expect(DEFAULTS.limit).toBe(100);

    const parsed = parsePageState(new URLSearchParams("limit=25"));
    expect(parsed.limit).toBe(100);
  });

  it("returns undefined for the all-time range", () => {
    expect(timeRangeToFromTime("all")).toBeUndefined();
    expect(timeRangeToFromTime("1h")).toMatch(/T/);
  });
});
