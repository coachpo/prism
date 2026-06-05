import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const dashboardMessages = {
  routingNoActiveRoutes: "No active routes",
  routingNoActiveRoutesDescription:
    "Create an entry model and enable at least one terminal target so Prism can publish this selected-profile routing topology.",
  routingNoRecentTraffic: "No routed traffic in the last 24h",
  routingNoRecentTrafficDescription:
    "The selected profile already has routes, but Prism recorded no successful terminal-target traffic in the last 24 hours.",
};
const routingInspectorMessages = {
  dashboard: {
    ...dashboardMessages,
    routingNodeType: "Node type",
    routingActiveConnections: "Active connections",
    routing24hSuccessRate: "24h success rate",
    routingLegendNoData: "No data",
    routing24hTotalRequests: "24h total requests",
    routing24hHealth: "24h health",
    routing24hSuccessfulRequests: "24h successful requests",
    routing24hErrors: "24h errors",
    routingActionOpenModelDetail: "Open model detail",
    reviewRequests: "Review requests",
    routingLegendHealthy: "Healthy",
    routingLegendDegraded: "Degraded",
    routingLegendFailing: "Failing",
    routingEndpointNodeType: "Endpoint",
    routingModelNodeType: "Model",
  },
  requestLogs: {
    view: "View",
  },
  modelDetail: {
    connections: "Connections",
    healthHealthy: "Healthy",
    healthUnknown: "Unknown",
    healthUnhealthy: "Unhealthy",
  },
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/i18n/format": {
      compareStringsForLocale: (left, right) => String(left).localeCompare(String(right), "en"),
    },
    "@/i18n/staticMessages": {
      getStaticMessages: () => ({ dashboard: dashboardMessages }),
    },
    "@/lib/types": {
      getTerminalTargetId: (value) => value.terminal_target_id ?? value.connection_id ?? null,
    },
  },
});
const {
  getRoutingDiagramEmptyState,
  getRoutingDiagramGraph,
  getRoutingDiagramMobileData,
  getRoutingDiagramSummary,
} = load(path.join(frontendDir, "src/pages/dashboard/routingDiagram.ts"));

function formatNumber(value, locale = "en-US", options = undefined) {
  return new Intl.NumberFormat(locale, options).format(value);
}

function loadRoutingDiagramInspectorContentModule() {
  const { load: loadInspector } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/i18n/format": {
        compareStringsForLocale: (left, right) => String(left).localeCompare(String(right), "en"),
        formatNumber,
        getCurrentLocale: () => "en-US",
      },
      "@/i18n/useLocale": {
        useLocale: () => ({
          formatNumber: (value) => formatNumber(value),
          messages: routingInspectorMessages,
        }),
      },
    },
  });

  return loadInspector(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramInspectorContent.tsx"),
  );
}

function renderRoutingDiagramInspectorContent(props) {
  const { RoutingDiagramInspectorContent } = loadRoutingDiagramInspectorContentModule();
  return renderToStaticMarkup(createElement(RoutingDiagramInspectorContent, props));
}

test("normalizes topology graph into renderer-agnostic graph", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const nodeById = new Map(graph.nodes.map((node) => [node.id, node]));
  const edgeById = new Map(graph.edges.map((edge) => [edge.id, edge]));

  assert.deepEqual(
    graph.nodes.map((node) => ({ id: node.id, kind: node.kind })),
    [
      { id: "model-102", kind: "model" },
      { id: "model-101", kind: "model" },
      { id: "terminal-target-502", kind: "terminal_target" },
      { id: "terminal-target-501", kind: "terminal_target" },
      { id: "endpoint-201", kind: "endpoint" },
    ],
  );
  assert.deepEqual(
    graph.edges.map((edge) => ({ id: edge.id, kind: edge.kind })),
    [
      { id: "access-target-1001", kind: "model_to_model" },
      { id: "access-target-1002", kind: "model_to_terminal_target" },
      { id: "access-target-1003", kind: "model_to_terminal_target" },
      { id: "terminal-target-binding-501", kind: "terminal_target_to_endpoint" },
      { id: "terminal-target-binding-502", kind: "terminal_target_to_endpoint" },
    ],
  );

  const primaryModel = nodeById.get("model-101");
  assert.ok(primaryModel);
  assert.equal(primaryModel.activeTerminalTargetCount, 1);
  assert.equal(primaryModel.requestCount24h, 42);
  assert.equal(primaryModel.successCount24h, 41);
  assert.equal(primaryModel.errorCount24h, 1);
  assert.ok(primaryModel.successRate24h !== null);
  assert.ok(Math.abs(primaryModel.successRate24h - 97.61904761904762) < 0.000001);

  const terminalBinding = edgeById.get("terminal-target-binding-501");
  assert.ok(terminalBinding);
  assert.equal(terminalBinding.sourceNodeId, "terminal-target-501");
  assert.equal(terminalBinding.targetNodeId, "endpoint-201");
  assert.equal(terminalBinding.activeTerminalTargetCount, 1);
  assert.equal(terminalBinding.requestCount24h, 42);

  for (const node of graph.nodes) {
    assert.equal(Object.prototype.hasOwnProperty.call(node, "value"), false, `${node.id} should not expose Sankey node value`);
  }

  for (const edge of graph.edges) {
    assert.equal(Object.prototype.hasOwnProperty.call(edge, "source"), false, `${edge.id} should not expose Sankey source index`);
    assert.equal(Object.prototype.hasOwnProperty.call(edge, "target"), false, `${edge.id} should not expose Sankey target index`);
    assert.equal(Object.prototype.hasOwnProperty.call(edge, "value"), false, `${edge.id} should not expose Sankey edge value`);
  }

  const mobileData = getRoutingDiagramMobileData(graph);
  assert.deepEqual(
    mobileData.sections.map((section) => ({ kind: section.kind, ids: section.nodes.map((node) => node.id) })),
    [
      { kind: "model", ids: ["model-102", "model-101"] },
      { kind: "terminal_target", ids: ["terminal-target-502", "terminal-target-501"] },
      { kind: "endpoint", ids: ["endpoint-201"] },
    ],
  );

  const endpoint = mobileData.sections[2]?.nodes[0];
  assert.ok(endpoint);
  assert.deepEqual(
    endpoint.incoming.map((relation) => relation.nodeId),
    ["terminal-target-501", "terminal-target-502"],
  );
});

test("returns stable empty graph for empty topology", () => {
  const data = createEmptyTopologyGraph();
  const graph = getRoutingDiagramGraph(data);

  assert.deepEqual(graph, { nodes: [], edges: [] });
  assert.deepEqual(getRoutingDiagramMobileData(graph), { sections: [] });
  assert.deepEqual(getRoutingDiagramSummary(data), {
    endpointCount: 0,
    modelCount: 0,
    activeTargetCount: 0,
    recentRequestTotal24h: 0,
  });
  assert.deepEqual(getRoutingDiagramEmptyState(data), {
    kind: "no_active_routes",
    title: dashboardMessages.routingNoActiveRoutes,
    description: dashboardMessages.routingNoActiveRoutesDescription,
  });
});

test("keeps shell semantics derived from raw topology data when invalid edges collapse the graph", () => {
  const data = createTopologyGraphWithInvalidEdge();
  const graph = getRoutingDiagramGraph(data);

  assert.deepEqual(graph.edges, []);
  assert.deepEqual(
    graph.nodes.map((node) => node.id),
    ["model-101", "terminal-target-501"],
  );
  assert.deepEqual(getRoutingDiagramSummary(data), {
    endpointCount: 1,
    modelCount: 1,
    activeTargetCount: 1,
    recentRequestTotal24h: 7,
  });
  assert.deepEqual(getRoutingDiagramEmptyState(data), {
    kind: "no_recent_traffic",
    title: dashboardMessages.routingNoRecentTraffic,
    description: dashboardMessages.routingNoRecentTrafficDescription,
  });
});

test("renders node inspector content from explicit graph node input", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const node = graph.nodes.find((item) => item.id === "model-101");

  assert.ok(node);
  const markup = renderRoutingDiagramInspectorContent({ node });

  assert.match(markup, /Model A/);
  assert.match(markup, /model-a/);
  assert.match(markup, /Node type/);
  assert.match(markup, /Model/);
  assert.match(markup, /Active connections/);
  assert.match(markup, /24h success rate/);
  assert.match(markup, /97\.62%/);
  assert.match(markup, /24h total requests/);
  assert.match(markup, /View/);
  assert.match(markup, /Open model detail/);
});

test("renders edge inspector content from explicit graph edge input", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const edge = graph.edges.find((item) => item.id === "terminal-target-binding-501");

  assert.ok(edge);
  const markup = renderRoutingDiagramInspectorContent({ edge });

  assert.match(markup, /Primary Target/);
  assert.match(markup, /Endpoint A/);
  assert.match(markup, /24h health/);
  assert.match(markup, /Degraded/);
  assert.match(markup, /24h success rate/);
  assert.match(markup, /97\.62%/);
  assert.match(markup, /24h successful requests/);
  assert.match(markup, /24h errors/);
});

function createTopologyGraph() {
  return {
    nodes: [
      {
        id: "model-101",
        kind: "model",
        label: "Model A",
        sublabel: "model-a",
        status: "enabled",
        model_config_id: 101,
        model_id: "model-a",
      },
      {
        id: "model-102",
        kind: "model",
        label: "Disabled Model",
        sublabel: "disabled-model",
        status: "disabled",
        model_config_id: 102,
        model_id: "disabled-model",
      },
      {
        id: "terminal-target-501",
        kind: "connection",
        product_kind: "terminal_target",
        label: "Primary Target",
        sublabel: "Endpoint A",
        status: "active",
        terminal_target_id: 501,
        connection_id: 501,
        active: true,
        health_status: "healthy",
        recent_request_count: 42,
        recent_success_rate: 97.6,
        last_request_at: "2026-04-10T12:34:56Z",
      },
      {
        id: "terminal-target-502",
        kind: "connection",
        product_kind: "terminal_target",
        label: "Backup Target",
        sublabel: "Endpoint A",
        status: "inactive",
        terminal_target_id: 502,
        connection_id: 502,
        active: false,
        health_status: "unknown",
        recent_request_count: 0,
        recent_success_rate: null,
        last_request_at: null,
      },
      {
        id: "endpoint-201",
        kind: "endpoint",
        label: "Endpoint A",
        sublabel: "Endpoint 201",
        status: "configured",
        endpoint_id: 201,
      },
    ],
    edges: [
      {
        id: "access-target-1001",
        kind: "model_to_model",
        source_node_id: "model-101",
        target_node_id: "model-102",
        position: 0,
        enabled: true,
      },
      {
        id: "access-target-1002",
        kind: "model_to_connection",
        product_kind: "model_to_terminal_target",
        source_node_id: "model-101",
        target_node_id: "terminal-target-501",
        position: 1,
        enabled: true,
      },
      {
        id: "access-target-1003",
        kind: "model_to_connection",
        product_kind: "model_to_terminal_target",
        source_node_id: "model-101",
        target_node_id: "terminal-target-502",
        position: 2,
        enabled: true,
      },
      {
        id: "terminal-target-binding-501",
        kind: "connection_to_endpoint",
        product_kind: "terminal_target_to_endpoint",
        source_node_id: "terminal-target-501",
        target_node_id: "endpoint-201",
      },
      {
        id: "terminal-target-binding-502",
        kind: "connection_to_endpoint",
        product_kind: "terminal_target_to_endpoint",
        source_node_id: "terminal-target-502",
        target_node_id: "endpoint-201",
      },
    ],
    stats: {
      model_count: 2,
      active_model_count: 1,
      disabled_model_count: 1,
      terminal_target_count: 2,
      active_terminal_target_count: 1,
      inactive_terminal_target_count: 1,
      endpoint_count: 1,
      edge_count: 5,
    },
  };
}

function createEmptyTopologyGraph() {
  return {
    nodes: [],
    edges: [],
    stats: {
      model_count: 0,
      active_model_count: 0,
      disabled_model_count: 0,
      terminal_target_count: 0,
      active_terminal_target_count: 0,
      inactive_terminal_target_count: 0,
      endpoint_count: 0,
      edge_count: 0,
    },
  };
}

function createTopologyGraphWithInvalidEdge() {
  return {
    nodes: [
      {
        id: "model-101",
        kind: "model",
        label: "Model A",
        sublabel: "model-a",
        status: "enabled",
        model_config_id: 101,
        model_id: "model-a",
      },
      {
        id: "terminal-target-501",
        kind: "connection",
        product_kind: "terminal_target",
        label: "Primary Target",
        sublabel: "Endpoint A",
        status: "active",
        terminal_target_id: 501,
        connection_id: 501,
        active: true,
        health_status: "healthy",
        recent_request_count: 7,
        recent_success_rate: 100,
        last_request_at: "2026-04-10T12:34:56Z",
      },
    ],
    edges: [
      {
        id: "broken-access-target",
        kind: "model_to_connection",
        product_kind: "model_to_terminal_target",
        source_node_id: "model-101",
        target_node_id: "missing-terminal-target",
        enabled: true,
      },
    ],
    stats: {
      model_count: 1,
      active_model_count: 1,
      disabled_model_count: 0,
      terminal_target_count: 1,
      active_terminal_target_count: 1,
      inactive_terminal_target_count: 0,
      endpoint_count: 1,
      edge_count: 1,
    },
  };
}
