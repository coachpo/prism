import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach } from "vitest";

interface StorageLike {
  clear: () => void;
  getItem: (key: string) => string | null;
  key: (index: number) => string | null;
  readonly length: number;
  removeItem: (key: string) => void;
  setItem: (key: string, value: string) => void;
}

function createMemoryStorage(): StorageLike {
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

const testLocalStorage =
  typeof window !== "undefined" && isStorageLike(window.localStorage)
    ? window.localStorage
    : createMemoryStorage();

Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: testLocalStorage,
});

if (typeof window !== "undefined") {
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: testLocalStorage,
  });
}

beforeEach(() => {
  testLocalStorage.clear();
});

afterEach(() => {
  testLocalStorage.clear();
});
