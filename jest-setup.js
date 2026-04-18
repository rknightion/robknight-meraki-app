// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';

// Polyfill IntersectionObserver for jsdom — @grafana/scenes imports
// components (LazyLoader) that touch it at module load time, so even
// tests that don't render scenes need the global defined.
if (typeof global.IntersectionObserver === 'undefined') {
  class IntersectionObserverMock {
    observe() {
      /* noop */
    }
    unobserve() {
      /* noop */
    }
    disconnect() {
      /* noop */
    }
    takeRecords() {
      return [];
    }
  }
  global.IntersectionObserver = IntersectionObserverMock;
}

// ResizeObserver is also missing in jsdom and trips parts of the Grafana UI
// component tree when imported in tests.
if (typeof global.ResizeObserver === 'undefined') {
  class ResizeObserverMock {
    observe() {
      /* noop */
    }
    unobserve() {
      /* noop */
    }
    disconnect() {
      /* noop */
    }
  }
  global.ResizeObserver = ResizeObserverMock;
}
