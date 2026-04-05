import type { ButtonHTMLAttributes } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ConnectionCard } from "./ConnectionCard";
import type { SortableConnectionCardProps } from "./connectionCardTypes";

export function SortableConnectionCard({
  connection,
  reorderDisabled = false,
  ...props
}: SortableConnectionCardProps) {
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: connection.id,
    disabled: reorderDisabled,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div ref={setNodeRef} style={style}>
      <ConnectionCard
        {...props}
        connection={connection}
        isDragging={isDragging}
        reorderDisabled={reorderDisabled}
        dragHandleAttributes={attributes as ButtonHTMLAttributes<HTMLButtonElement>}
        dragHandleListeners={listeners as ButtonHTMLAttributes<HTMLButtonElement> | undefined}
        dragHandleRef={setActivatorNodeRef}
      />
    </div>
  );
}
