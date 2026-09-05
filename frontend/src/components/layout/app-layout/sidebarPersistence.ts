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

/**
 * 1024–1279（含 14 寸笔记本 125% 缩放）这一档最吃亏：240px 侧栏占掉近四分之一
 * 宽度，而内容区正好放不下驾驶舱表格。这一档默认收成图标轨，操作者手动展开过
 * 就按他的选择来。
 */
const TABLET_MEDIA_QUERY = "(max-width: 1279px)";

export function readSidebarCollapsed(): boolean {
  const storage = getLocalStorage();
  const stored = storage?.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY);
  if (stored !== null && stored !== undefined) {
    return stored === "true";
  }

  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  return window.matchMedia(TABLET_MEDIA_QUERY).matches;
}

export function writeSidebarCollapsed(collapsed: boolean): void {
  const storage = getLocalStorage();
  if (!storage) {
    return;
  }

  storage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, collapsed ? "true" : "false");
}
