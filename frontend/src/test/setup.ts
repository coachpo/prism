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

const testLocalStorage = createMemoryStorage();

const canvasContextStub = {
  beginPath: () => {},
  fillRect: () => {},
  lineCap: "round",
  lineJoin: "round",
  lineTo: () => {},
  lineWidth: 1,
  moveTo: () => {},
  setTransform: () => {},
  stroke: () => {},
  strokeStyle: "",
  fillStyle: "",
} as unknown as CanvasRenderingContext2D;

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

if (typeof HTMLCanvasElement !== "undefined") {
  Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
    configurable: true,
    value: () => canvasContextStub,
  });
}

if (typeof ResizeObserver === "undefined") {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  Object.defineProperty(globalThis, "ResizeObserver", {
    configurable: true,
    value: ResizeObserverStub,
  });
}

beforeEach(() => {
  testLocalStorage.clear();
});

afterEach(() => {
  testLocalStorage.clear();
});
