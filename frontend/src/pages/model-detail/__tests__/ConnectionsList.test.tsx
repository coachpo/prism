import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Connection, ModelConfig, Vendor } from "@/lib/types";
import { ConnectionsList } from "../ConnectionsList";

const sortableCardProps: Record<string, unknown>[] = [];
const overlayCardProps: Record<string, unknown>[] = [];

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => `formatted:${value}`,
  }),
}));

vi.mock("../connections-list/SortableConnectionCard", () => ({
  SortableConnectionCard: (props: Record<string, unknown>) => {
    sortableCardProps.push(props);
    return <div>{`sortable-card:${String(props.connection && (props.connection as { id: number }).id)}`}</div>;
  },
}));

vi.mock("../connections-list/ConnectionCard", () => ({
  ConnectionCard: (props: Record<string, unknown>) => {
    overlayCardProps.push(props);
    return <div>{`overlay-card:${String(props.connection && (props.connection as { id: number }).id)}`}</div>;
  },
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children, onDragStart }: { children: React.ReactNode; onDragStart?: (event: { active: { id: number } }) => void }) => (
    <div>
      <button type="button" onClick={() => onDragStart?.({ active: { id: 11 } })}>
        start-drag
      </button>
      {children}
    </div>
  ),
  DragOverlay: ({ children }: { children: React.ReactNode }) => <div data-testid="drag-overlay">{children}</div>,
  KeyboardSensor: class KeyboardSensor {},
  PointerSensor: class PointerSensor {},
  TouchSensor: class TouchSensor {},
  closestCenter: () => null,
  useSensor: () => ({}),
  useSensors: (...sensors: unknown[]) => sensors,
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  sortableKeyboardCoordinates: () => null,
  verticalListSortingStrategy: {},
}));

function buildVendor(): Vendor {
  return {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
  };
}

function buildModel(): ModelConfig {
  return {
    id: 5,
    vendor_id: 1,
    vendor: buildVendor(),
    api_family: "openai",
    model_id: "gpt-5.4",
    display_name: "GPT-5.4",
    model_type: "native",
    is_enabled: true,
    proxy_targets: [],
    connections: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
  };
}

function buildConnection(id: number, name: string): Connection {
  return {
    id,
    model_config_id: 5,
    endpoint_id: id + 100,
    endpoint: {
      id: id + 100,
      name: `${name} endpoint`,
      base_url: `https://api-${id}.example.com/v1`,
      has_api_key: true,
      masked_api_key: "sk-••••",
      position: 0,
      created_at: "2026-03-20T10:00:00Z",
      updated_at: "2026-03-20T10:00:00Z",
    },
    is_active: true,
    priority: id,
    name,
    auth_type: null,
    custom_headers: null,
    pricing_template_id: null,
    pricing_template: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    health_status: "healthy",
    health_detail: null,
    last_health_check: null,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
  };
}

function renderSubject() {
  const connections = [
    buildConnection(11, "Primary"),
    buildConnection(12, "Secondary"),
    buildConnection(13, "Tertiary"),
    buildConnection(14, "Quaternary"),
  ];

  return render(
    <LocaleProvider>
      <ConnectionsList
        model={buildModel()}
        connections={connections}
        connectionSearch=""
        setConnectionSearch={vi.fn()}
        openConnectionDialog={vi.fn()}
        handleDeleteConnection={vi.fn()}
        handleHealthCheck={vi.fn()}
        handleToggleActive={vi.fn()}
        handleReorderConnections={vi.fn().mockResolvedValue(undefined)}
        currentStateByConnectionId={new Map()}
        resettingConnectionIds={new Set<number>()}
        healthCheckingIds={new Set<number>()}
        focusedConnectionId={null}
        connectionCardRefs={new Map<number, HTMLDivElement>()}
        reorderInFlight={false}
        handleResetCooldown={vi.fn()}
      />
    </LocaleProvider>,
  );
}

describe("ConnectionsList", () => {
  it("keeps the simplified connection list shell and drag overlay without monitoring props", () => {
    sortableCardProps.length = 0;
    overlayCardProps.length = 0;

    renderSubject();

    expect(screen.queryByRole("button", { name: "Load 24h Metrics" })).not.toBeInTheDocument();
    expect(
      screen.queryByText("Connection metrics and health checks load on demand to avoid large page-open bursts."),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Check All" })).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText("Filter connections...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add Connection" })).toBeInTheDocument();

    expect(sortableCardProps[0]).toMatchObject({ connection: expect.objectContaining({ id: 11 }) });
    expect(sortableCardProps[0]).not.toHaveProperty("metrics24h");
    expect(sortableCardProps[0]).not.toHaveProperty("monitoringConnection");
    expect(sortableCardProps[0]).not.toHaveProperty("monitoringLoading");

    fireEvent.click(screen.getByRole("button", { name: "start-drag" }));

    expect(overlayCardProps.at(-1)).toMatchObject({ connection: expect.objectContaining({ id: 11 }) });
    expect(overlayCardProps.at(-1)).not.toHaveProperty("monitoringConnection");
    expect(overlayCardProps.at(-1)).not.toHaveProperty("monitoringLoading");
    expect(overlayCardProps.at(-1)).not.toHaveProperty("metrics24h");
  });
});
