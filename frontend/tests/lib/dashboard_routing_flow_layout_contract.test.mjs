import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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
    routingActiveTerminalTargets: "Active terminal targets",
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
    healthHealthy: "Healthy",
    healthUnknown: "Unknown",
    healthUnhealthy: "Unhealthy",
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
  getRoutingDiagramEmptyState,
  getRoutingDiagramFlowLayout,
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

function loadRoutingDiagramFlowNodeModule() {
  const { load: loadFlowNode } = createTsModuleLoader({
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
          messages: routingInspectorMessages,
        }),
      },
      "@/lib/utils": {
        cn: (...parts) => parts.filter(Boolean).join(" "),
      },
    },
  });

  return loadFlowNode(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramFlowNode.tsx"),
  );
}

function renderRoutingDiagramFlowNode(props) {
  const { RoutingDiagramFlowNode } = loadRoutingDiagramFlowNodeModule();
  return renderToStaticMarkup(createElement(RoutingDiagramFlowNode, props));
}

function loadRoutingDiagramFlowEdgeModule() {
  const { load: loadFlowEdge } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@xyflow/react": {
        BaseEdge: (props) => {
          const { interactionWidth, path, ...rest } = props;
          return createElement("path", {
            ...rest,
            d: path,
            "data-hit-area-width": interactionWidth ?? undefined,
          });
        },
        getBezierPath: ({ sourceX, sourceY, targetX, targetY }) => [
          `M ${sourceX},${sourceY} C ${sourceX + 44},${sourceY} ${targetX - 44},${targetY} ${targetX},${targetY}`,
          sourceX,
          sourceY,
          targetX,
          targetY,
        ],
      },
      "@/i18n/useLocale": {
        useLocale: () => ({
          messages: {
            dashboard: {
              routingLink: "Routing link",
              routingLinkAria: (sourceLabel, targetLabel) => `${sourceLabel} to ${targetLabel}`,
            },
          },
        }),
      },
    },
  });

  return loadFlowEdge(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramFlowEdge.tsx"),
  );
}

function renderRoutingDiagramFlowEdge(props) {
  const { RoutingDiagramFlowEdge } = loadRoutingDiagramFlowEdgeModule();
  return renderToStaticMarkup(createElement(RoutingDiagramFlowEdge, props));
}

function loadRoutingDiagramFlowEdgeStyleModule() {
  const { load: loadFlowEdgeStyle } = createTsModuleLoader({
    rootDir: frontendDir,
  });

  return loadFlowEdgeStyle(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/routingDiagramFlowEdgeStyle.ts"),
  );
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
  assert.match(markup, /24h health/);
  assert.match(markup, /Degraded/);
  assert.match(markup, /24h success rate/);
  assert.match(markup, /97\.62%/);
  assert.match(markup, /24h successful requests/);
  assert.match(markup, /24h errors/);
});

test("produces deterministic flow layout positions", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const firstLayout = getRoutingDiagramFlowLayout(graph);
  const secondLayout = getRoutingDiagramFlowLayout(graph);

  assert.deepEqual(secondLayout, firstLayout);
  assert.deepEqual(summarizeFlowNodes(firstLayout), [
    { id: "model-101", x: 40, y: 24, width: 224, height: 176 },
    { id: "model-102", x: 432, y: 24, width: 224, height: 176 },
    { id: "terminal-target-501", x: 432, y: 224, width: 208, height: 160 },
    { id: "terminal-target-502", x: 432, y: 408, width: 208, height: 160 },
    { id: "endpoint-201", x: 824, y: 24, width: 224, height: 176 },
  ]);
  assert.deepEqual(
    firstLayout.edges.map((edge) => ({ id: edge.id, source: edge.source, target: edge.target })),
    [
      { id: "access-target-1001", source: "model-101", target: "model-102" },
      { id: "access-target-1002", source: "model-101", target: "terminal-target-501" },
      { id: "access-target-1003", source: "model-101", target: "terminal-target-502" },
      { id: "terminal-target-binding-501", source: "terminal-target-501", target: "endpoint-201" },
      { id: "terminal-target-binding-502", source: "terminal-target-502", target: "endpoint-201" },
    ],
  );
  assert.deepEqual(firstLayout.bounds, { x: 0, y: 0, width: 1088, height: 592 });
});

test("stably orders tied nodes by position label and id", () => {
  const layout = getRoutingDiagramFlowLayout(createStableOrderingGraph());

  assert.deepEqual(summarizeFlowNodes(layout), [
    { id: "model-alpha", x: 40, y: 24, width: 224, height: 176 },
    { id: "model-beta", x: 40, y: 224, width: 224, height: 176 },
    { id: "terminal-positioned", x: 432, y: 24, width: 208, height: 160 },
    { id: "terminal-alpha-1", x: 432, y: 208, width: 208, height: 160 },
    { id: "terminal-alpha-2", x: 432, y: 392, width: 208, height: 160 },
    { id: "terminal-beta-1", x: 432, y: 576, width: 208, height: 160 },
  ]);
  assert.deepEqual(
    getRoutingDiagramFlowLayout(createStableOrderingGraph()).nodes.map((node) => node.id),
    layout.nodes.map((node) => node.id),
  );
  assert.deepEqual(layout.bounds, { x: 0, y: 0, width: 680, height: 760 });
});

test("applies fallback placement for cycles or orphan nodes", () => {
  const layout = getRoutingDiagramFlowLayout(createFallbackRoutingGraph());

  assert.deepEqual(summarizeFlowNodes(layout), [
    { id: "cycle-model-a", x: 40, y: 24, width: 224, height: 176 },
    { id: "orphan-model", x: 40, y: 224, width: 224, height: 176 },
    { id: "cycle-model-b", x: 432, y: 24, width: 224, height: 176 },
    { id: "orphan-terminal-target", x: 432, y: 224, width: 208, height: 160 },
    { id: "connected-terminal-target", x: 824, y: 24, width: 208, height: 160 },
    { id: "orphan-endpoint", x: 824, y: 208, width: 224, height: 176 },
    { id: "connected-endpoint", x: 1216, y: 24, width: 224, height: 176 },
  ]);
  assert.deepEqual(layout.edges.map((edge) => edge.id), [
    "cycle-edge-a-b",
    "cycle-edge-b-a",
    "cycle-target-edge",
    "connected-binding",
  ]);
  assert.deepEqual(layout.bounds, { x: 0, y: 0, width: 1480, height: 424 });
});

test("renders interactive flow node buttons with stable test ids", () => {
  const modelMarkup = renderRoutingDiagramFlowNode({
    data: {
      ...createGraphNode({
        id: "model-101",
        kind: "model",
        label: "Model A",
        sublabel: "model-a",
        modelConfigId: 101,
        modelId: "model-a",
        status: "disabled",
      }),
      requestCount24h: 42,
      successCount24h: 41,
      errorCount24h: 1,
      successRate24h: 97.61904761904762,
    },
    onActivateNode: () => {},
  });
  const endpointMarkup = renderRoutingDiagramFlowNode({
    data: createGraphNode({
      id: "endpoint-201",
      kind: "endpoint",
      label: "Endpoint A",
      sublabel: "https://endpoint-a.example/v1",
      endpointId: 201,
      activeTerminalTargetCount: 2,
      requestCount24h: 42,
      successCount24h: 41,
      errorCount24h: 1,
      successRate24h: 97.61904761904762,
    }),
    onActivateNode: () => {},
  });

  assert.match(modelMarkup, /data-testid="routing-diagram-node-model-model-101"/);
  assert.match(modelMarkup, /data-muted="true"/);
  assert.match(modelMarkup, /style="[^"]*--routing-node-color:var\(--chart-1\)[^"]*background:linear-gradient/);
  assert.doesNotMatch(modelMarkup, /size-2\.5 shrink-0 rounded-full/);
  assert.match(modelMarkup, /<button[^>]*type="button"[^>]*class="[^"]*nodrag[^"]*nopan[^"]*"[^>]*aria-label="View Model Details: Model A"/);
  assert.match(modelMarkup, /View Model Details: Model A/);
  assert.match(modelMarkup, /model-a/);
  assert.match(modelMarkup, /Disabled/);

  assert.match(endpointMarkup, /data-testid="routing-diagram-node-endpoint-endpoint-201"/);
  assert.match(endpointMarkup, /style="[^"]*--routing-node-color:var\(--chart-2\)[^"]*background:linear-gradient/);
  assert.doesNotMatch(endpointMarkup, /size-2\.5 shrink-0 rounded-full/);
  assert.match(endpointMarkup, /<button[^>]*type="button"[^>]*class="[^"]*nodrag[^"]*nopan[^"]*"[^>]*aria-label="View Request Logs: Endpoint A"/);
  assert.match(endpointMarkup, /<span class="block w-full truncate">View Request Logs<\/span><\/button>/);
  assert.match(endpointMarkup, /2 active targets/);
});

test("renders terminal target node without button semantics", () => {
  const markup = renderRoutingDiagramFlowNode({
    data: createGraphNode({
      id: "terminal-target-501",
      kind: "terminal_target",
      label: "Primary Target",
      sublabel: "Endpoint A",
      terminalTargetId: 501,
      active: false,
      status: "inactive",
      activeTerminalTargetCount: 1,
      requestCount24h: 42,
      successCount24h: 41,
      errorCount24h: 1,
      successRate24h: 97.61904761904762,
    }),
    onActivateNode: () => {},
  });

  assert.match(markup, /data-testid="routing-diagram-node-terminal-target-terminal-target-501"/);
  assert.match(markup, /data-muted="true"/);
  assert.match(markup, /style="[^"]*--routing-node-color:var\(--chart-4\)[^"]*background:linear-gradient/);
  assert.doesNotMatch(markup, /size-2\.5 shrink-0 rounded-full/);
  assert.doesNotMatch(markup, /<button/);
  assert.doesNotMatch(markup, /role="button"/);
  assert.doesNotMatch(markup, /tabindex=/i);
  assert.match(markup, /Endpoint A/);
  assert.match(markup, /Terminal Targets/);
  assert.match(markup, /Inactive/);
  assert.match(markup, /41 successful requests in 24h/);
});

test("maps edge health to bezier style values", () => {
  const { getRoutingDiagramFlowEdgeStyle } = loadRoutingDiagramFlowEdgeStyleModule();
  const healthyStyle = getRoutingDiagramFlowEdgeStyle(
    createGraphEdge({
      id: "access-target-1002",
      kind: "model_to_terminal_target",
      sourceNodeId: "model-101",
      targetNodeId: "terminal-target-501",
      sourceLabel: "Model A",
      targetLabel: "Primary Target",
      activeTerminalTargetCount: 4,
      requestCount24h: 42,
      successCount24h: 42,
      errorCount24h: 0,
      successRate24h: 99.25,
    }),
  );
  const noTrafficStyle = getRoutingDiagramFlowEdgeStyle(
    createGraphEdge({
      id: "terminal-target-binding-502",
      kind: "terminal_target_to_endpoint",
      sourceNodeId: "terminal-target-502",
      targetNodeId: "endpoint-201",
      sourceLabel: "Backup Target",
      targetLabel: "Endpoint A",
      activeTerminalTargetCount: 8,
      requestCount24h: 0,
      successCount24h: 0,
      errorCount24h: 0,
      successRate24h: null,
    }),
  );
  const markup = renderRoutingDiagramFlowEdge({
    id: "access-target-1002",
    sourceX: 12,
    sourceY: 24,
    targetX: 212,
    targetY: 96,
    sourcePosition: "right",
    targetPosition: "left",
    data: createGraphEdge({
      id: "access-target-1002",
      kind: "model_to_terminal_target",
      sourceNodeId: "model-101",
      targetNodeId: "terminal-target-501",
      sourceLabel: "Model A",
      targetLabel: "Primary Target",
      activeTerminalTargetCount: 4,
      requestCount24h: 42,
      successCount24h: 42,
      errorCount24h: 0,
      successRate24h: 99.25,
    }),
  });

  assert.deepEqual(healthyStyle, {
    stroke: "#10b981",
    strokeOpacity: 0.38,
    strokeWidth: 5,
  });
  assert.deepEqual(noTrafficStyle, {
    stroke: "#64748b",
    strokeOpacity: 0.24,
    strokeWidth: 6,
  });
  assert.match(markup, /data-testid="routing-diagram-edge-access-target-1002"/);
  assert.match(markup, /data-testid="routing-diagram-edge-hit-area-access-target-1002"/);
  assert.match(markup, /aria-label="Model A to Primary Target"/);
  assert.match(markup, /data-hit-area-width="24"/);
  assert.match(markup, /d="M 12,24 C 56,24 168,96 212,96"/);
  assert.doesNotMatch(markup, /role="button"/);
  assert.doesNotMatch(markup, /onclick=/i);
});

test("keeps stable edge ids across repeated flow adaptation", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const firstLayout = getRoutingDiagramFlowLayout(graph);
  const secondLayout = getRoutingDiagramFlowLayout(graph);

  assert.deepEqual(
    firstLayout.edges.map((edge) => edge.id),
    [
      "access-target-1001",
      "access-target-1002",
      "access-target-1003",
      "terminal-target-binding-501",
      "terminal-target-binding-502",
    ],
  );
  assert.deepEqual(
    firstLayout.edges.map((edge) => edge.id),
    secondLayout.edges.map((edge) => edge.id),
  );
  assert.deepEqual(
    firstLayout.edges.map((edge) => edge.data.id),
    firstLayout.edges.map((edge) => edge.id),
  );
});

function loadRoutingDiagramFlowModule() {
  let capturedReactFlowProps = null;

  const { load: loadFlow } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@xyflow/react": {
        Controls: ({ orientation, showInteractive }) =>
          createElement("div", {
            "data-testid": "mock-flow-controls",
            "data-orientation": orientation ?? "vertical",
            "data-show-interactive": String(showInteractive ?? true),
          }),
        Handle: ({ position, style, type }) =>
          createElement("span", {
            "data-testid": "mock-flow-handle",
            "data-handle-position": position,
            "data-handle-style": JSON.stringify(style ?? {}),
            "data-handle-type": type,
          }),
        Position: { Left: "left", Right: "right" },
        ReactFlow: (props) => {
          capturedReactFlowProps = props;
          const renderedNodes = (props.nodes ?? []).map((node) => {
            const NodeComponent = props.nodeTypes?.[node.type];
            return NodeComponent
              ? createElement(NodeComponent, { key: node.id, data: node.data, id: node.id })
              : null;
          });
          const renderedEdges = (props.edges ?? []).map((edge) => {
            const EdgeComponent = props.edgeTypes?.[edge.type];
            return EdgeComponent
              ? createElement(EdgeComponent, {
                  key: edge.id,
                  data: edge.data,
                  id: edge.id,
                  sourceX: 12,
                  sourceY: 24,
                  targetX: 212,
                  targetY: 96,
                })
              : null;
          });

          return createElement(
            "div",
            { "data-testid": "mock-react-flow" },
            renderedNodes,
            renderedEdges,
            props.children,
          );
        },
        useNodesState: (initialNodes) => [initialNodes, () => {}, () => {}],
      },
      "./RoutingDiagramFlowEdge": {
        RoutingDiagramFlowEdge: ({ data, id }) =>
          createElement("div", { "data-testid": `mock-flow-edge-${data?.id ?? id}` }, data?.id ?? id),
      },
      "./RoutingDiagramFlowNode": {
        RoutingDiagramFlowNode: ({ data, onActivateNode }) =>
          createElement("article", {
            "data-testid": `mock-flow-node-${data.id}`,
            "data-has-activate": typeof onActivateNode === "function" ? "true" : "false",
          }, data.label),
      },
      "./RoutingDiagramInspectorContent": {
        RoutingDiagramInspectorContent: ({ edge, node }) =>
          createElement("div", { "data-testid": "mock-routing-diagram-inspector-content" }, node?.id ?? edge?.id ?? ""),
      },
      "./RoutingDiagramLegend": {
        RoutingDiagramLegend: () => createElement("div", { "data-testid": "mock-routing-diagram-legend" }, "Legend"),
      },
      "./RoutingDiagramVisualizationShell": {
        RoutingDiagramVisualizationShell: ({ children, visualization }) =>
          createElement("section", { "data-testid": "mock-routing-diagram-shell" }, children, visualization),
      },
    },
  });

  return {
    ...loadFlow(
      path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramFlow.tsx"),
    ),
    getCapturedReactFlowProps: () => capturedReactFlowProps,
  };
}

function renderRoutingDiagramFlow(props) {
  const { RoutingDiagramFlow, getCapturedReactFlowProps } = loadRoutingDiagramFlowModule();
  const markup = renderToStaticMarkup(createElement(RoutingDiagramFlow, props));

  return {
    markup,
    reactFlowProps: getCapturedReactFlowProps(),
  };
}

function loadRoutingDiagramFlowStateModule() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });

  return load(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/routingDiagramFlowState.ts"),
  );
}

test("imports React Flow stylesheet once from main entrypoint", () => {
  const source = readFileSync(path.join(frontendDir, "src/main.tsx"), "utf8");
  const matches = source.match(/@xyflow\/react\/dist\/style\.css/g) ?? [];

  assert.equal(matches.length, 1);
});

test("renders interactive desktop flow surface with controls and draggable nodes", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const flowLayout = getRoutingDiagramFlowLayout(graph);
  const { markup, reactFlowProps } = renderRoutingDiagramFlow({
    chartHeight: 420,
    graphData: graph,
    onActivateNode: () => {},
  });

  assert.match(markup, /data-testid="mock-routing-diagram-shell"/);
  assert.match(markup, /data-testid="routing-diagram-desktop"/);
  assert.match(markup, /data-testid="routing-diagram-inspector"/);
  assert.match(markup, /style="height:420px"/);
  assert.match(markup, /data-testid="mock-routing-diagram-legend"/);
  assert.match(markup, /data-testid="mock-flow-controls"/);
  assert.doesNotMatch(markup, /class="nodrag nopan"/);
  assert.equal((markup.match(/data-testid="mock-flow-handle"/g) ?? []).length, graph.nodes.length * 2);
  assert.ok(reactFlowProps);
  assert.equal(reactFlowProps.fitView, true);
  assert.equal(reactFlowProps.minZoom, 0.35);
  assert.ok(reactFlowProps.maxZoom > 1);
  assert.equal(reactFlowProps.nodesDraggable, true);
  assert.equal(reactFlowProps.nodesConnectable, false);
  assert.equal(reactFlowProps.elementsSelectable, false);
  assert.equal(reactFlowProps.nodesFocusable, false);
  assert.equal(reactFlowProps.edgesFocusable, false);
  assert.equal(reactFlowProps.disableKeyboardA11y, true);
  assert.equal(reactFlowProps.panOnDrag, true);
  assert.equal(reactFlowProps.panOnScroll, false);
  assert.equal(reactFlowProps.zoomOnScroll, true);
  assert.equal(reactFlowProps.zoomOnPinch, true);
  assert.equal(reactFlowProps.zoomOnDoubleClick, false);
  assert.deepEqual(reactFlowProps.proOptions, { hideAttribution: true });
  assert.deepEqual(
    reactFlowProps.nodes.map((node) => ({
      draggable: node.draggable,
      focusable: node.focusable,
      id: node.id,
      selectable: node.selectable,
      type: node.type,
    })),
    flowLayout.nodes.map((node) => ({
      draggable: true,
      focusable: false,
      id: node.id,
      selectable: false,
      type: "routing-diagram-node",
    })),
  );
});

test("preserves dragged node positions across same-layout flow refreshes", () => {
  const graph = getRoutingDiagramGraph(createTopologyGraph());
  const refreshedTopology = createTopologyGraph();
  refreshedTopology.nodes[2].recent_request_count = 84;
  refreshedTopology.nodes[2].recent_success_rate = 98.8;
  const refreshedGraph = getRoutingDiagramGraph(refreshedTopology);
  const { reactFlowProps: initialReactFlowProps } = renderRoutingDiagramFlow({
    chartHeight: 420,
    graphData: graph,
    onActivateNode: () => {},
  });
  const { reactFlowProps: refreshedReactFlowProps } = renderRoutingDiagramFlow({
    chartHeight: 420,
    graphData: refreshedGraph,
    onActivateNode: () => {},
  });
  const {
    getRoutingDiagramFlowLayoutSignature,
    reconcileRoutingDiagramFlowNodes,
  } = loadRoutingDiagramFlowStateModule();

  assert.equal(
    getRoutingDiagramFlowLayoutSignature(getRoutingDiagramFlowLayout(graph)),
    getRoutingDiagramFlowLayoutSignature(getRoutingDiagramFlowLayout(refreshedGraph)),
  );

  const draggedNodes = initialReactFlowProps.nodes.map((node) =>
    node.id === "model-101"
      ? {
          ...node,
          position: {
            x: node.position.x + 72,
            y: node.position.y + 48,
          },
        }
      : node,
  );
  const reconciledNodes = reconcileRoutingDiagramFlowNodes(
    draggedNodes,
    refreshedReactFlowProps.nodes,
  );
  const draggedModelNode = draggedNodes.find((node) => node.id === "model-101");
  const reconciledModelNode = reconciledNodes.find((node) => node.id === "model-101");
  const refreshedModelNode = refreshedGraph.nodes.find((node) => node.id === "model-101");

  assert.ok(draggedModelNode);
  assert.ok(reconciledModelNode);
  assert.ok(refreshedModelNode);
  assert.deepEqual(reconciledModelNode.position, draggedModelNode.position);
  assert.equal(reconciledModelNode.data.graphNode.requestCount24h, refreshedModelNode.requestCount24h);
  assert.equal(reconciledModelNode.data.graphNode.successRate24h, refreshedModelNode.successRate24h);
});

test("keeps inspector seam and desktop controls without extra editor chrome", () => {
  const source = readFileSync(
    path.join(frontendDir, "src/pages/dashboard/routing-diagram/RoutingDiagramFlow.tsx"),
    "utf8",
  );

  assert.match(source, /data-testid="routing-diagram-inspector"/);
  assert.match(source, /onPointerLeave=\{[^}]+\}/);
  assert.match(source, /Escape/);
  assert.match(source, /\bControls\b/);
  assert.match(source, /showInteractive=\{false\}/);
  assert.doesNotMatch(source, /\bMiniMap\b/);
});

function summarizeFlowNodes(layout) {
  return layout.nodes.map((node) => ({
    id: node.id,
    x: node.position.x,
    y: node.position.y,
    width: node.width,
    height: node.height,
  }));
}

function createStableOrderingGraph() {
  return {
    nodes: [
      createGraphNode({
        id: "terminal-alpha-2",
        kind: "terminal_target",
        label: "Alpha Shared",
        sublabel: "Endpoint A",
        terminalTargetId: 502,
      }),
      createGraphNode({
        id: "model-beta",
        kind: "model",
        label: "Beta Root",
        modelConfigId: 202,
        modelId: "beta-root",
      }),
      createGraphNode({
        id: "terminal-positioned",
        kind: "terminal_target",
        label: "Position First",
        sublabel: "Endpoint A",
        terminalTargetId: 501,
      }),
      createGraphNode({
        id: "model-alpha",
        kind: "model",
        label: "Alpha Root",
        modelConfigId: 201,
        modelId: "alpha-root",
      }),
      createGraphNode({
        id: "terminal-beta-1",
        kind: "terminal_target",
        label: "Aardvark Shared",
        sublabel: "Endpoint B",
        terminalTargetId: 503,
      }),
      createGraphNode({
        id: "terminal-alpha-1",
        kind: "terminal_target",
        label: "Alpha Shared",
        sublabel: "Endpoint A",
        terminalTargetId: 504,
      }),
    ],
    edges: [
      createGraphEdge({
        id: "edge-alpha-null-2",
        kind: "model_to_terminal_target",
        sourceNodeId: "model-alpha",
        targetNodeId: "terminal-alpha-2",
        sourceLabel: "Alpha Root",
        targetLabel: "Alpha Shared",
      }),
      createGraphEdge({
        id: "edge-beta-null",
        kind: "model_to_terminal_target",
        sourceNodeId: "model-beta",
        targetNodeId: "terminal-beta-1",
        sourceLabel: "Beta Root",
        targetLabel: "Aardvark Shared",
      }),
      createGraphEdge({
        id: "edge-alpha-positioned",
        kind: "model_to_terminal_target",
        sourceNodeId: "model-alpha",
        targetNodeId: "terminal-positioned",
        sourceLabel: "Alpha Root",
        targetLabel: "Position First",
        position: 0,
      }),
      createGraphEdge({
        id: "edge-alpha-null-1",
        kind: "model_to_terminal_target",
        sourceNodeId: "model-alpha",
        targetNodeId: "terminal-alpha-1",
        sourceLabel: "Alpha Root",
        targetLabel: "Alpha Shared",
      }),
    ],
  };
}

function createFallbackRoutingGraph() {
  return {
    nodes: [
      createGraphNode({
        id: "connected-endpoint",
        kind: "endpoint",
        label: "Connected Endpoint",
        endpointId: 401,
      }),
      createGraphNode({
        id: "orphan-endpoint",
        kind: "endpoint",
        label: "Orphan Endpoint",
        endpointId: 402,
      }),
      createGraphNode({
        id: "orphan-terminal-target",
        kind: "terminal_target",
        label: "Orphan Target",
        sublabel: "Endpoint O",
        terminalTargetId: 601,
      }),
      createGraphNode({
        id: "cycle-model-b",
        kind: "model",
        label: "Cycle Model B",
        modelConfigId: 302,
        modelId: "cycle-model-b",
      }),
      createGraphNode({
        id: "connected-terminal-target",
        kind: "terminal_target",
        label: "Connected Target",
        sublabel: "Endpoint C",
        terminalTargetId: 602,
      }),
      createGraphNode({
        id: "orphan-model",
        kind: "model",
        label: "Orphan Model",
        modelConfigId: 303,
        modelId: "orphan-model",
      }),
      createGraphNode({
        id: "cycle-model-a",
        kind: "model",
        label: "Cycle Model A",
        modelConfigId: 301,
        modelId: "cycle-model-a",
      }),
    ],
    edges: [
      createGraphEdge({
        id: "cycle-edge-b-a",
        kind: "model_to_model",
        sourceNodeId: "cycle-model-b",
        targetNodeId: "cycle-model-a",
        sourceLabel: "Cycle Model B",
        targetLabel: "Cycle Model A",
        position: 1,
      }),
      createGraphEdge({
        id: "broken-source-edge",
        kind: "model_to_terminal_target",
        sourceNodeId: "missing-model",
        targetNodeId: "connected-terminal-target",
        sourceLabel: "Missing Model",
        targetLabel: "Connected Target",
        position: 0,
      }),
      createGraphEdge({
        id: "connected-binding",
        kind: "terminal_target_to_endpoint",
        sourceNodeId: "connected-terminal-target",
        targetNodeId: "connected-endpoint",
        sourceLabel: "Connected Target",
        targetLabel: "Connected Endpoint",
      }),
      createGraphEdge({
        id: "cycle-target-edge",
        kind: "model_to_terminal_target",
        sourceNodeId: "cycle-model-b",
        targetNodeId: "connected-terminal-target",
        sourceLabel: "Cycle Model B",
        targetLabel: "Connected Target",
        position: 0,
      }),
      createGraphEdge({
        id: "cycle-edge-a-b",
        kind: "model_to_model",
        sourceNodeId: "cycle-model-a",
        targetNodeId: "cycle-model-b",
        sourceLabel: "Cycle Model A",
        targetLabel: "Cycle Model B",
        position: 0,
      }),
      createGraphEdge({
        id: "broken-target-edge",
        kind: "terminal_target_to_endpoint",
        sourceNodeId: "connected-terminal-target",
        targetNodeId: "missing-endpoint",
        sourceLabel: "Connected Target",
        targetLabel: "Missing Endpoint",
      }),
    ],
  };
}

function createGraphNode(overrides) {
  return {
    id: "graph-node",
    kind: "model",
    label: "Graph Node",
    sublabel: null,
    status: "enabled",
    modelConfigId: null,
    modelId: null,
    terminalTargetId: null,
    endpointId: null,
    active: null,
    healthStatus: null,
    activeTerminalTargetCount: 0,
    requestCount24h: 0,
    successCount24h: 0,
    errorCount24h: 0,
    successRate24h: null,
    lastRequestAt: null,
    ...overrides,
  };
}

function createGraphEdge(overrides) {
  return {
    id: "graph-edge",
    kind: "model_to_model",
    sourceNodeId: "source-node",
    targetNodeId: "target-node",
    sourceLabel: "Source Node",
    targetLabel: "Target Node",
    enabled: true,
    position: null,
    activeTerminalTargetCount: 0,
    requestCount24h: 0,
    successCount24h: 0,
    errorCount24h: 0,
    successRate24h: null,
    ...overrides,
  };
}

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
