import { beforeEach, describe, expect, it, vi } from "vitest";
import { createConnectionNavigator } from "../connectionNavigation";

const api = vi.hoisted(() => ({
  connections: {
    owner: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}));

describe("createConnectionNavigator", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("re-reads connection ownership instead of reusing stale cached owners", async () => {
    const navigate = vi.fn();
    api.connections.owner
      .mockResolvedValueOnce({ model_config_id: 11 })
      .mockResolvedValueOnce({ model_config_id: 12 });

    const navigateToConnection = createConnectionNavigator({
      navigate,
    });

    await navigateToConnection(42);
    await navigateToConnection(42);

    expect(api.connections.owner).toHaveBeenCalledTimes(2);
    expect(navigate).toHaveBeenNthCalledWith(1, "/models/11?focus_connection_id=42");
    expect(navigate).toHaveBeenNthCalledWith(2, "/models/12?focus_connection_id=42");
  });
});
