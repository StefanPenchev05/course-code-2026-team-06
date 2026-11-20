import '@testing-library/jest-dom/vitest';

// Test-only shim. jsdom installs its own AbortSignal, but Request still comes
// from Node's built-in fetch (undici), which rejects jsdom's signal. React Router
// builds a Request with a signal on every navigation, so redirects (<Navigate>,
// navigate()) would crash in tests. Tests never abort a navigation, so dropping
// the signal is safe here. Browsers are not affected.
const NodeRequest = globalThis.Request;
globalThis.Request = class extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    const { signal: _signal, ...rest } = init ?? {};
    super(input, rest);
  }
} as typeof Request;
