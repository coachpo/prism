import { createContext, useContext, useEffect } from "react";

/**
 * Breadcrumbs are fixed at group -> page -> entity, and a detail page's leaf
 * must be the entity's real name rather than a generic word. Only the page
 * knows that name, so it publishes it here while the shell keeps ownership of
 * how breadcrumbs are assembled.
 */
export const BreadcrumbEntityValueContext = createContext<string | null>(null);
export const BreadcrumbEntitySetterContext = createContext<
  ((label: string | null) => void) | null
>(null);

export function useBreadcrumbEntity(): string | null {
  return useContext(BreadcrumbEntityValueContext);
}

/**
 * Publish the current page's entity name. Pass `null` while the name is still
 * loading — the shell falls back to the identifier rather than inventing one.
 */
export function usePublishBreadcrumbEntity(label: string | null | undefined): void {
  const setLabel = useContext(BreadcrumbEntitySetterContext);
  const normalized = label?.trim() ? label.trim() : null;

  useEffect(() => {
    if (!setLabel) return;
    setLabel(normalized);
    return () => setLabel(null);
  }, [normalized, setLabel]);
}
