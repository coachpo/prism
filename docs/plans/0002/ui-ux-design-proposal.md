# UI/UX Design Proposal: Endpoint Drag-and-Drop Ordering

This document outlines the UI/UX design for the drag-and-drop endpoint ordering feature in Prism, adhering to the existing React, Tailwind, and shadcn/ui stack.

## 1. Interaction Model
- **Explicit Drag Handle:** To prevent accidental drags when users are trying to select text or click buttons, dragging will be restricted to a dedicated drag handle (`GripVertical` icon) rather than the entire card.
- **Optimistic Updates:** When a card is dropped, the UI will immediately reflect the new order. The API call will happen in the background.
- **Silent Success, Loud Failure:** Successful reordering will not show a toast (to avoid notification fatigue). If the API call fails, the UI will revert to the previous order and display an error toast.
- **Single In-Flight Request:** While a reorder API call is pending, further drag operations will be temporarily disabled or queued to prevent race conditions.

## 2. Visual Design & Layout Tweaks
- **Grid Layout:** The existing CSS grid (`grid gap-4 sm:grid-cols-2 xl:grid-cols-3`) is perfectly suited for `@dnd-kit`'s `rectSortingStrategy`.
- **Helper Copy:** Add a subtle hint below the page header description or near the stats cards: *"Drag and drop cards using the handle to reorder your endpoints."*

## 3. Card Anatomy & Drag Handle
The drag handle will be integrated into the `CardHeader`.

**Changes to CardHeader:**
- Add a `GripVertical` icon on the far left.
- Adjust padding to ensure the handle feels integrated but distinct from the title.

```tsx
<CardHeader className="flex flex-row items-start gap-3 space-y-0 pb-3">
  {/* Drag Handle */}
  <div
    className="mt-0.5 flex cursor-grab items-center justify-center rounded-md p-1 text-muted-foreground/40 transition-colors hover:bg-muted hover:text-foreground active:cursor-grabbing"
    aria-label={`Reorder ${endpoint.name}`}
  >
    <GripVertical className="h-5 w-5" />
  </div>

  {/* Title & Badge */}
  <div className="min-w-0 flex-1 space-y-2">
    <CardTitle className="truncate text-base font-semibold">
      {endpoint.name}
    </CardTitle>
    <Badge variant="outline" className="...">
      {getEndpointHost(endpoint.base_url)}
    </Badge>
  </div>

  {/* Actions (Duplicate, Edit, Delete) */}
  <div className="flex shrink-0 items-center gap-1">
    {/* ... */}
  </div>
</CardHeader>
```

## 4. States & Visual Feedback
Using `@dnd-kit`, we will apply specific Tailwind classes based on the drag state:

- **Default:** Handle is subtle (`text-muted-foreground/40`).
- **Hover (Handle):** Handle darkens (`text-foreground`), background gets a subtle highlight (`bg-muted`), cursor becomes `grab`.
- **Active Dragging (The item being moved):**
  - Cursor becomes `grabbing`.
  - Add a strong shadow and border to lift it off the page: `shadow-xl ring-2 ring-primary/50 border-primary/50`.
  - Slightly scale up (optional, e.g., `scale-[1.02]`) for a tactile feel.
  - Increase `z-index` to ensure it floats above other cards.
- **Drop Placeholder (The space left behind):**
  - The original position of the dragged item should remain visible but muted to show where it will land.
  - Apply `opacity-30` and `border-dashed`.

## 5. Keyboard Accessibility
- The drag handle must be focusable (`tabIndex={0}` is usually handled by `@dnd-kit`'s `useSortable`).
- Include an `aria-label` on the handle: `"Drag to reorder endpoint {endpoint.name}"`.
- `@dnd-kit` provides built-in keyboard sensors. Users can focus the handle, press `Space` or `Enter` to pick it up, use `Arrow` keys to move it across the grid, and press `Space` to drop (or `Escape` to cancel).
- Ensure a visible focus ring (`focus-visible:ring-2 focus-visible:ring-ring`) is present on the drag handle for keyboard users.

## 6. Mobile & Touch Considerations
- **Touch Sensor:** Use `@dnd-kit`'s `TouchSensor` with a slight delay (e.g., `delay: 250`, `tolerance: 5`) so that normal scrolling doesn't accidentally trigger a drag.
- **Touch Target:** Ensure the drag handle has a minimum touch target of 44x44px on mobile devices (achieved via padding).

## 7. Implementation Scope (Practicality)
- **Dependencies:** `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities`.
- **Components:**
  - Create a `SortableEndpointCard` wrapper component that consumes `useSortable` and renders the existing `Card` with the necessary `ref`, `style` (transform), and `listeners`.
  - Wrap the grid in `<DndContext>` and `<SortableContext strategy={rectSortingStrategy}>`.
  - Use `<DragOverlay>` to render the floating card during the drag operation (crucial for grid layouts to prevent layout shifting glitches).

## 8. Edge Cases
- **Empty State:** Drag and drop is disabled/hidden when there are 0 or 1 endpoints.
- **Loading State:** Skeletons remain unchanged.
- **Pagination/Filtering:** If added in the future, ordering usually only applies to the global list. Currently, Prism shows all endpoints, so this is safe.
