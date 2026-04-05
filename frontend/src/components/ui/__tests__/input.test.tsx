import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Input } from "../input";

describe("Input", () => {
  it("defaults autocomplete to off", () => {
    render(<Input aria-label="Internal field" />);

    expect(screen.getByLabelText("Internal field")).toHaveAttribute("autocomplete", "off");
  });

  it("preserves explicit autocomplete overrides", () => {
    render(<Input aria-label="URL field" autoComplete="url" />);

    expect(screen.getByLabelText("URL field")).toHaveAttribute("autocomplete", "url");
  });
});
