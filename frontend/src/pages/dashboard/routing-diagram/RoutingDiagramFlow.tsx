import {
  useCallback,
  useMemo,
  useState,
  type FocusEvent,
  type KeyboardEvent,
  type MouseEvent as ReactMouseEvent,
} from "react";
import {
  Handle,
  Position,
  ReactFlow,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";

import type {
  RoutingDiagramGraph,
  RoutingDiagramGraphEdge,
  RoutingDiagramGraphNode,
} from "./routingDiagramContracts";
import {
  getRoutingDiagramFlowLayout,
  ROUTING_DIAGRAM_FLOW_EDGE_TYPE,
  ROUTING_DIAGRAM_FLOW_NODE_TYPE,
} from "./routingDiagramFlowLayout";
import { RoutingDiagramFlowEdge } from "./RoutingDiagramFlowEdge";
import { RoutingDiagramFlowNode } from "./RoutingDiagramFlowNode";
import { RoutingDiagramInspectorContent } from "./RoutingDiagramInspectorContent";
import { RoutingDiagramLegend } from "./RoutingDiagramLegend";
import { RoutingDiagramVisualizationShell } from "./RoutingDiagramVisualizationShell";

interface RoutingDiagramFlowProps {
  graphData: RoutingDiagramGraph;
  chartHeight: number;
  onActivateNode?: (node: RoutingDiagramGraphNode) => void;
}

type RoutingDiagramFlowCanvasNodeData = {
  graphNode: RoutingDiagramGraphNode;
  onActivateNode?: (node: RoutingDiagramGraphNode) => void;
};

type RoutingDiagramFlowCanvasEdgeData = {
  graphEdge: RoutingDiagramGraphEdge;
  onInspectEdge: (edge: RoutingDiagramGraphEdge) => void;
};

type RoutingDiagramFlowCanvasNode = Node<
  RoutingDiagramFlowCanvasNodeData,
  typeof ROUTING_DIAGRAM_FLOW_NODE_TYPE
>;
type RoutingDiagramFlowCanvasEdge = Edge<
  RoutingDiagramFlowCanvasEdgeData,
  typeof ROUTING_DIAGRAM_FLOW_EDGE_TYPE
>;

type RoutingDiagramInspectorState =
  | { node: RoutingDiagramGraphNode; edge?: never }
  | { edge: RoutingDiagramGraphEdge; node?: never }
  | null;

const HIDDEN_HANDLE_STYLE = {
  background: "transparent",
  border: 0,
  height: 1,
  opacity: 0,
  pointerEvents: "none",
  width: 1,
} as const;

const nodeTypes = {
  [ROUTING_DIAGRAM_FLOW_NODE_TYPE]: RoutingDiagramFlowCanvasNodeComponent,
};

const edgeTypes = {
  [ROUTING_DIAGRAM_FLOW_EDGE_TYPE]: RoutingDiagramFlowCanvasEdgeComponent,
};

export function RoutingDiagramFlow({
  graphData,
  chartHeight,
  onActivateNode,
}: RoutingDiagramFlowProps) {
  const [inspector, setInspector] = useState<RoutingDiagramInspectorState>(null);
  const flowLayout = useMemo(() => getRoutingDiagramFlowLayout(graphData), [graphData]);
  const graphNodeById = useMemo(() => {
    return new Map(graphData.nodes.map((node) => [node.id, node]));
  }, [graphData.nodes]);

  const nodes = useMemo<RoutingDiagramFlowCanvasNode[]>(() => {
    return flowLayout.nodes.map((node) => ({
      id: node.id,
      type: node.type,
      data: { graphNode: node.data, onActivateNode },
      position: node.position,
      width: node.width,
      height: node.height,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      className: "bg-transparent border-0 shadow-none",
      draggable: false,
      selectable: false,
      focusable: false,
      style: {
        background: "transparent",
        border: "none",
        height: node.height,
        width: node.width,
      },
    }));
  }, [flowLayout.nodes, onActivateNode]);

  const clearInspector = useCallback(() => {
    setInspector(null);
  }, []);

  const showEdgeInspector = useCallback((edge: RoutingDiagramGraphEdge) => {
    setInspector({ edge });
  }, []);

  const edges = useMemo<RoutingDiagramFlowCanvasEdge[]>(() => {
    return flowLayout.edges.map((edge) => ({
      id: edge.id,
      type: edge.type,
      source: edge.source,
      target: edge.target,
      data: { graphEdge: edge.data, onInspectEdge: showEdgeInspector },
      focusable: false,
      reconnectable: false,
      selectable: false,
    }));
  }, [flowLayout.edges, showEdgeInspector]);

  const handleNodeMouseEnter = useCallback(
    (_event: ReactMouseEvent, node: RoutingDiagramFlowCanvasNode) => {
      setInspector({ node: node.data.graphNode });
    },
    [],
  );

  const handleEdgeMouseEnter = useCallback(
    (_event: ReactMouseEvent, edge: RoutingDiagramFlowCanvasEdge) => {
      if (!edge.data) {
        return;
      }

      setInspector({ edge: edge.data.graphEdge });
    },
    [],
  );

  const handleFocusCapture = useCallback(
    (event: FocusEvent<HTMLDivElement>) => {
      if (!(event.target instanceof Element)) {
        return;
      }

      const flowNode = event.target.closest<HTMLElement>(".react-flow__node[data-id]");
      const nodeId = flowNode?.dataset.id;
      if (!nodeId) {
        return;
      }

      const node = graphNodeById.get(nodeId);
      if (node) {
        setInspector({ node });
      }
    },
    [graphNodeById],
  );

  const handleKeyDownCapture = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "Escape") {
        clearInspector();
      }
    },
    [clearInspector],
  );

  return (
    <RoutingDiagramVisualizationShell
      visualization={
        <div
          data-testid="routing-diagram-desktop"
          className="relative overflow-hidden rounded-xl border border-border/70 bg-background/60"
          style={{ height: chartHeight }}
          onPointerLeave={clearInspector}
          onFocusCapture={handleFocusCapture}
          onKeyDownCapture={handleKeyDownCapture}
        >
          <ReactFlow<RoutingDiagramFlowCanvasNode, RoutingDiagramFlowCanvasEdge>
            fitView
            minZoom={0.35}
            maxZoom={1}
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={false}
            nodesFocusable={false}
            edgesFocusable={false}
            disableKeyboardA11y={true}
            panOnDrag={false}
            panOnScroll={false}
            zoomOnScroll={false}
            zoomOnPinch={false}
            zoomOnDoubleClick={false}
            proOptions={{ hideAttribution: true }}
            onNodeMouseEnter={handleNodeMouseEnter}
            onEdgeMouseEnter={handleEdgeMouseEnter}
          />
          <div
            data-testid="routing-diagram-inspector"
            className="pointer-events-none absolute right-4 top-4 z-10"
          >
            {inspector?.node ? <RoutingDiagramInspectorContent node={inspector.node} /> : null}
            {inspector?.edge ? <RoutingDiagramInspectorContent edge={inspector.edge} /> : null}
          </div>
        </div>
      }
    >
      <RoutingDiagramLegend />
    </RoutingDiagramVisualizationShell>
  );
}

function RoutingDiagramFlowCanvasNodeComponent({
  data,
}: NodeProps<RoutingDiagramFlowCanvasNode>) {
  return (
    <>
      <Handle type="target" position={Position.Left} isConnectable={false} style={HIDDEN_HANDLE_STYLE} />
      <div className="nodrag nopan">
        <RoutingDiagramFlowNode data={data.graphNode} onActivateNode={data.onActivateNode} />
      </div>
      <Handle type="source" position={Position.Right} isConnectable={false} style={HIDDEN_HANDLE_STYLE} />
    </>
  );
}

function RoutingDiagramFlowCanvasEdgeComponent(props: EdgeProps<RoutingDiagramFlowCanvasEdge>) {
  if (!props.data) {
    return null;
  }

  return (
    <RoutingDiagramFlowEdge
      {...props}
      data={props.data.graphEdge}
      onInspectEdge={props.data.onInspectEdge}
    />
  );
}
