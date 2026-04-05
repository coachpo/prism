import { vi } from "vitest";

export function createLocalStorageMock(): Storage {
  let storage: Record<string, string> = {};

  return {
    clear: () => {
      storage = {};
    },
    getItem: (key) => storage[key] ?? null,
    key: (index) => Object.keys(storage)[index] ?? null,
    get length() {
      return Object.keys(storage).length;
    },
    removeItem: (key) => {
      delete storage[key];
    },
    setItem: (key, value) => {
      storage[key] = value;
    },
  };
}

export function installLocalStorageMock(): Storage {
  const localStorageMock = createLocalStorageMock();

  vi.stubGlobal("localStorage", localStorageMock);
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: localStorageMock,
  });

  return localStorageMock;
}
