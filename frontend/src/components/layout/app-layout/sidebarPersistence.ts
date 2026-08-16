export const SIDEBAR_COLLAPSED_STORAGE_KEY = "prism.sidebarCollapsed";

interface StorageLike {
  clear: () => void;
  getItem: (key: string) => string | null;
  key: (index: number) => string | null;
  readonly length: number;
  removeItem: (key: string) => void;
  setItem: (key: string, value: string) => void;
}

function isStorageLike(value: unknown): value is StorageLike {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as StorageLike).clear === "function" &&
    typeof (value as StorageLike).getItem === "function" &&
    typeof (value as StorageLike).key === "function" &&
    typeof (value as StorageLike).length === "number" &&
    typeof (value as StorageLike).removeItem === "function" &&
    typeof (value as StorageLike).setItem === "function"
  );
}

function getLocalStorage(): StorageLike | null {
  if (typeof window === "undefined" || !isStorageLike(window.localStorage)) {
    return null;
  }

  return window.localStorage;
}

export function readSidebarCollapsed(): boolean {
  const storage = getLocalStorage();
  if (!storage) {
    return false;
  }

  return storage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY) === "true";
}

export function writeSidebarCollapsed(collapsed: boolean): void {
  const storage = getLocalStorage();
  if (!storage) {
    return;
  }

  storage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, collapsed ? "true" : "false");
}
