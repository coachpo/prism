import { describe, expect, it } from "vitest";
import type { Vendor } from "@/lib/types";
import {
  normalizeVendorPayload,
  parseVendorUsageRows,
  vendorFormStateFromVendor,
} from "../vendorManagementFormState";

function buildVendor(overrides: Partial<Vendor> = {}): Vendor {
  return {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: "openai",
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-27T00:00:00Z",
    updated_at: "2026-03-27T00:00:00Z",
    ...overrides,
  };
}

describe("vendorManagementFormState", () => {
  it("hydrates edit state from a vendor while normalizing null description", () => {
    expect(vendorFormStateFromVendor(buildVendor({ description: null, icon_key: null }))).toEqual({
      key: "openai",
      name: "OpenAI",
      description: "",
      icon_key: null,
    });
  });

  it("trims payload fields and normalizes icon keys", () => {
    expect(
      normalizeVendorPayload({
        key: "  openai-enterprise  ",
        name: "  OpenAI Enterprise  ",
        description: "  Dedicated tenant  ",
        icon_key: "  OpenAI  ",
      }),
    ).toEqual({
      key: "openai-enterprise",
      name: "OpenAI Enterprise",
      description: "Dedicated tenant",
      icon_key: "openai",
    });

    expect(
      normalizeVendorPayload({
        key: "vendor",
        name: "Vendor",
        description: "   ",
        icon_key: "   ",
      }),
    ).toEqual({
      key: "vendor",
      name: "Vendor",
      description: null,
      icon_key: null,
    });
  });

  it("keeps only valid vendor usage rows from delete-conflict detail", () => {
    expect(
      parseVendorUsageRows({
        models: [
          {
            model_config_id: 9,
            profile_id: 3,
            profile_name: "Team Blue",
            model_id: "openai/gpt-4.1",
            display_name: "GPT-4.1",
            model_type: "native",
            api_family: "openai",
            is_enabled: true,
          },
          {
            model_config_id: 10,
            profile_id: "bad",
            profile_name: "Broken",
            model_id: "broken/model",
            display_name: null,
            model_type: "native",
            api_family: "openai",
            is_enabled: true,
          },
        ],
      }),
    ).toEqual([
      {
        model_config_id: 9,
        profile_id: 3,
        profile_name: "Team Blue",
        model_id: "openai/gpt-4.1",
        display_name: "GPT-4.1",
        model_type: "native",
        api_family: "openai",
        is_enabled: true,
      },
    ]);
  });
});
