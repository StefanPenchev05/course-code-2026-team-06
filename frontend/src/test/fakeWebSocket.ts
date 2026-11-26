/**
 * TEST DOUBLE — a minimal in-memory WebSocket for unit tests of the realtime
 * client. It is not the real gateway; real-backend behaviour is covered by the
 * Playwright journeys in tests/e2e.
 */
export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  readyState = 0;
  sent: unknown[] = [];
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  static reset() {
    FakeWebSocket.instances = [];
  }

  static last(): FakeWebSocket {
    return FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
  }

  send(data: unknown) {
    this.sent.push(data);
  }

  close(code = 1000) {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.({ code } as CloseEvent);
  }

  // Test controls
  open() {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }

  receive(data: string) {
    this.onmessage?.({ data } as MessageEvent);
  }

  /** The server or network closed the connection. */
  drop(code = 1006) {
    this.readyState = 3;
    this.onclose?.({ code } as CloseEvent);
  }
}

export function event(type: string, overrides: Record<string, unknown> = {}) {
  return JSON.stringify({
    version: 1,
    id: crypto.randomUUID(),
    type,
    ...(type === 'account.access_changed' ? {} : { entityId: '11111111-1111-4111-8111-111111111111' }),
    occurredAt: '2026-11-23T16:00:00Z',
    ...overrides,
  });
}
