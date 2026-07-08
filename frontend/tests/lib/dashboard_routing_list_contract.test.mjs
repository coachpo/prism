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
    "Create an entry model and enable at least one terminal target so Prism can publish this frozen Default-profile routing topology.",
  routingNoRecentTraffic: "No routed traffic in the last 24h",
  routingNoRecentTrafficDescription:
    "Frozen Default profile already has routes, but Prism recorded no successful terminal-target traffic in the last 24 hours.",
};
const routingInspectorMessages = {
  dashboard: {
    ...dashboardMessages,
    routingNodeType: "Node type",
    routingActiveTerminalTargets: "Active terminal targets",
    routing24hSuccessRate: "24h success rate",
    routingLegendNoData: "No data",
    routing24hTotalRequests: "24h total requests",
    routing24hStatus: "24h status",
    routing24hSuccessfulRequests: "24h successful requests",
    routing24hErrors: "24h errors",
    routingActionOpenModelDetail: "Open model detail",
    reviewRequests: "Review requests",
    routingLegendHealthy: "Healthy",
    routingLegendDegraded: "Degraded",
    routingLegendFailing: "Failing",
    routingEndpointNodeType: "Endpoint",
    routingModelNodeType: "Model",
    activeTargets: (value) => `${value} active targets`,
    successfulRequests24h: (value) => `${value} successful requests in 24h`,
  },
  requestLogs: {
    view: "View",
  },
  modelDetail: {
    active: "Active",
    connections: "Terminal Targets",
    disabled: "Disabled",
    inactive: "Inactive",
    viewRequestLogs: "View Request Logs",
  },
  modelsUi: {
    viewModelDetails: (label) => `View Model Details: ${label}`,
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
  filterRoutingDiagramGraphByModelIds,
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

function loadRoutingDiagramLegendModule() {
  const { load: loadLegend } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/i18n/format": {
        formatNumber,
        getCurrentLocale: () => "en-US",
      },
      "@/i18n/useLocale": {
        useLocale: () => ({
          formatNumber: (value) => formatNumber(value),
          messages: {
            ...routingInspectorMessages,
            dashboard: {
              ...routingInspectorMessages.dashboard,
              routingTitle: "Routing diagram",
            },
          },
        }),
      },
      "@/lib/utils": {
        cn: (...parts) => parts.filter(Boolean).join(" "),
      },
    },
  });

  return loadLegend(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramLegend.tsx"),
  );
}

function renderRoutingDiagramLegend() {
  const { RoutingDiagramLegend } = loadRoutingDiagramLegendModule();
  return renderToStaticMarkup(createElement(RoutingDiagramLegend));
}

function loadRoutingDiagramMobileListModule() {
  const { load: loadMobileList } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/components/ui/badge": {
        Badge: ({ children, ...props }) => createElement("span", props, children),
      },
      "@/components/ui/button": {
        Button: ({ children, ...props }) => createElement("button", props, children),
      },
      "@/i18n/format": {
        formatNumber,
        getCurrentLocale: () => "en-US",
      },
      "@/i18n/useLocale": {
        useLocale: () => ({
          formatNumber: (value) => formatNumber(value),
          messages: {
            ...routingInspectorMessages,
            dashboard: {
              ...routingInspectorMessages.dashboard,
              routingTitle: "Routing diagram",
            },
          },
        }),
      },
      "@/lib/utils": {
        cn: (...parts) => parts.filter(Boolean).join(" "),
      },
    },
  });

  return loadMobileList(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramMobileList.tsx"),
  );
}

function renderRoutingDiagramMobileList(props) {
  const { RoutingDiagramMobileList } = loadRoutingDiagramMobileListModule();
  return renderToStaticMarkup(createElement(RoutingDiagramMobileList, props));
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

  const terminalTarget = nodeById.get("terminal-target-501");
  assert.ok(terminalTarget);
  assert.equal(
    Object.prototype.hasOwnProperty.call(terminalTarget, "healthStatus"),
    false,
    "terminal target nodes should not expose probe-era health status",
  );

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

test("filters topology graph by selected model nodes", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());

  assert.strictEqual(
    filterRoutingDiagramGraphByModelIds(graph, new Set(["model-101", "model-102"])),
    graph,
  );

  assert.deepEqual(
    getGraphIds(filterRoutingDiagramGraphByModelIds(graph, new Set(["model-101"]))),
    {
      nodes: ["model-101", "terminal-target-502", "terminal-target-501", "endpoint-201"],
      edges: [
        "access-target-1002",
        "access-target-1003",
        "terminal-target-binding-501",
        "terminal-target-binding-502",
      ],
    },
  );

  assert.deepEqual(
    getGraphIds(filterRoutingDiagramGraphByModelIds(graph, new Set(["model-102"]))),
    {
      nodes: ["model-102"],
      edges: [],
    },
  );

  assert.deepEqual(
    filterRoutingDiagramGraphByModelIds(graph, new Set()),
    { nodes: [], edges: [] },
  );
});

function getGraphIds(graph) {
  return {
    nodes: graph.nodes.map((node) => node.id),
    edges: graph.edges.map((edge) => edge.id),
  };
}

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
  assert.match(markup, /Active terminal targets/);
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
  assert.match(markup, /24h status/);
  assert.match(markup, /Degraded/);
  assert.match(markup, /24h success rate/);
  assert.match(markup, /97\.62%/);
  assert.match(markup, /24h successful requests/);
  assert.match(markup, /24h errors/);
});

test("renders routing node kind markers with metadata-driven shapes", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const mobileData = getRoutingDiagramMobileData(graph);
  const legendMarkup = renderRoutingDiagramLegend();
  const mobileMarkup = renderRoutingDiagramMobileList({
    mobileData,
    onActivateNode: () => {},
    onInspectNode: () => {},
  });

  assert.match(legendMarkup, /class="[^"]*rounded-\[0\.2rem\][^"]*" data-node-shape="panel"/);
  assert.match(legendMarkup, /class="[^"]*rounded-full[^"]*" data-node-shape="capsule"/);
  assert.match(legendMarkup, /class="[^"]*\[clip-path:polygon\(0_0,70%_0,100%_30%,100%_100%,0_100%\)\][^"]*" data-node-shape="cut-corner"/);
  assert.match(legendMarkup, /data-node-shape="cut-corner" style="[^"]*clip-path:polygon\(0 0, 70% 0, 100% 30%, 100% 100%, 0 100%\)/);
  assert.doesNotMatch(legendMarkup, /clip-path:polygon\(0 0, calc\(100% - 0\.875rem\)/);

  assert.match(mobileMarkup, /data-testid="routing-diagram-mobile"/);
  assert.match(mobileMarkup, /class="[^"]*rounded-xl[^"]*" data-node-shape="panel" style="[^"]*--routing-node-color:var\(--chart-1\)[^"]*--routing-node-background:color-mix\(in oklab, var\(--chart-1\) 14%, var\(--background\)\)/);
  assert.match(mobileMarkup, /class="[^"]*rounded-\[1\.5rem\][^"]*" data-node-shape="capsule" style="[^"]*--routing-node-color:var\(--chart-2\)[^"]*--routing-node-background:color-mix\(in oklab, var\(--chart-2\) 16%, var\(--background\)\)/);
  assert.match(mobileMarkup, /class="[^"]*rounded-md[^"]*" data-node-shape="cut-corner" style="[^"]*--routing-node-color:var\(--chart-4\)[^"]*clip-path:polygon\(0 0, calc\(100% - 0\.875rem\) 0, 100% 0\.875rem, 100% 100%, 0 100%\)/);
  assert.match(mobileMarkup, /class="[^"]*\[clip-path:polygon\(0_0,70%_0,100%_30%,100%_100%,0_100%\)\][^"]*" data-node-shape="cut-corner" style="[^"]*clip-path:polygon\(0 0, 70% 0, 100% 30%, 100% 100%, 0 100%\)/);
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
