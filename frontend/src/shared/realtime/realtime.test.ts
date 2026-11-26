import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { event, FakeWebSocket } from '../../test/fakeWebSocket';
import { RealtimeClient, gatewayUrl } from './client';
import { parseEvent, RecentIds } from './protocol';

// Unit tests with a fake WebSocket (test double). The real gateway is covered in tests/e2e.

describe('parseEvent', () => {
  it('accepts every version-1 event type', () => {
    for (const type of ['booking.changed', 'availability.changed', 'tutor_application.changed', 'coordination.changed', 'activity.changed', 'account.access_changed']) {
      expect(parseEvent(event(type))).toMatchObject({ version: 1, type });
    }
  });

  it('ignores malformed, oversized and other-version frames without throwing', () => {
    expect(parseEvent('{"version":1')).toBeNull();
    expect(parseEvent('null')).toBeNull();
    expect(parseEvent('[]')).toBeNull();
    expect(parseEvent(event('booking.changed', { version: 2 }))).toBeNull();
    expect(parseEvent(event('booking.changed', { id: 'NOT-A-UUID' }))).toBeNull();
    expect(parseEvent(event('booking.changed', { id: '00000000-0000-0000-0000-000000000000' }))).toBeNull();
    expect(parseEvent(event('booking.changed', { entityId: undefined }))).toBeNull();
    expect(parseEvent(event('account.access_changed', { entityId: '11111111-1111-4111-8111-111111111111' }))).toBeNull();
    expect(parseEvent(event('booking.changed', { occurredAt: '2026-11-23 16:00' }))).toBeNull();
    expect(parseEvent(event('booking.changed', { payload: { status: 'ACCEPTED' } }))).toBeNull();
    expect(parseEvent(event('booking.changed', { entityId: 'x'.repeat(2000) }))).toBeNull();
    expect(parseEvent(new ArrayBuffer(4))).toBeNull();
  });

  it('marks a valid frame of a newer type as unknown', () => {
    expect(parseEvent(event('chat.message'))).toMatchObject({ type: 'unknown' });
  });
});

describe('RecentIds', () => {
  it('reports repeats and forgets the oldest beyond its size', () => {
    const ids = new RecentIds(2);
    expect(ids.add('a')).toBe(true);
    expect(ids.add('a')).toBe(false);
    ids.add('b');
    ids.add('c');
    expect(ids.add('a')).toBe(true);
  });
});

describe('RealtimeClient', () => {
  const created: RealtimeClient[] = [];
  beforeEach(() => {
    FakeWebSocket.reset();
    vi.useFakeTimers();
  });
  afterEach(() => {
    // Stop every client so none keeps listening for 'online' in later tests.
    created.splice(0).forEach((c) => c.stop());
    vi.useRealTimers();
  });

  function client(options: Partial<ConstructorParameters<typeof RealtimeClient>[0]> = {}) {
    const c = new RealtimeClient({ url: 'ws://localhost/ws', WebSocketImpl: FakeWebSocket as unknown as typeof WebSocket, random: () => 1, ...options });
    created.push(c);
    return c;
  }

  it('opens one socket, tells listeners about events, and never sends anything', () => {
    const onConnected = vi.fn();
    const c = client({ onConnected });
    const booking = vi.fn();
    const all = vi.fn();
    c.subscribe('booking.changed', booking);
    c.subscribe('*', all);
    c.start();
    c.start(); // no second socket

    expect(FakeWebSocket.instances).toHaveLength(1);
    FakeWebSocket.last().open();
    expect(onConnected).toHaveBeenCalledWith(1);
    expect(c.currentStatus).toBe('open');

    FakeWebSocket.last().receive(event('booking.changed'));
    FakeWebSocket.last().receive(event('availability.changed'));
    expect(booking).toHaveBeenCalledTimes(1);
    expect(all).toHaveBeenCalledTimes(2);
    expect(FakeWebSocket.last().sent).toEqual([]);
  });

  it('delivers a repeated event once and ignores malformed frames', () => {
    const c = client();
    const listener = vi.fn();
    c.subscribe('booking.changed', listener);
    c.start();
    FakeWebSocket.last().open();
    const repeated = event('booking.changed');
    FakeWebSocket.last().receive(repeated);
    FakeWebSocket.last().receive(repeated);
    FakeWebSocket.last().receive('not json');
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('keeps working when one listener throws', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const c = client();
    const good = vi.fn();
    c.subscribe('booking.changed', () => {
      throw new Error('broken view');
    });
    c.subscribe('booking.changed', good);
    c.start();
    FakeWebSocket.last().open();
    FakeWebSocket.last().receive(event('booking.changed'));
    expect(good).toHaveBeenCalledTimes(1);
  });

  it('reconnects with growing, capped delays and reports every new connection', () => {
    const onConnected = vi.fn();
    const c = client({ onConnected, baseDelayMs: 1000, maxDelayMs: 4000 });
    c.start();
    FakeWebSocket.last().open();

    FakeWebSocket.last().drop();
    expect(c.currentStatus).toBe('reconnecting');
    vi.advanceTimersByTime(999);
    expect(FakeWebSocket.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeWebSocket.instances).toHaveLength(2); // 1s

    FakeWebSocket.last().drop();
    vi.advanceTimersByTime(2000);
    expect(FakeWebSocket.instances).toHaveLength(3); // 2s
    FakeWebSocket.last().drop();
    vi.advanceTimersByTime(4000);
    expect(FakeWebSocket.instances).toHaveLength(4); // 4s
    FakeWebSocket.last().drop();
    vi.advanceTimersByTime(4000);
    expect(FakeWebSocket.instances).toHaveLength(5); // capped at 4s

    FakeWebSocket.last().open();
    expect(onConnected).toHaveBeenLastCalledWith(2);
    // A successful connection resets the backoff.
    FakeWebSocket.last().drop();
    vi.advanceTimersByTime(1000);
    expect(FakeWebSocket.instances).toHaveLength(6);
  });

  it('adds jitter: delays fall between half and all of the backoff', () => {
    expect(client({ random: () => 0, baseDelayMs: 1000 }).nextDelay()).toBe(500);
    expect(client({ random: () => 1, baseDelayMs: 1000 }).nextDelay()).toBe(1000);
  });

  it('asks the app to re-check the session when a connection is refused or closed for policy', () => {
    const onRejected = vi.fn();
    const c = client({ onRejected });
    c.start();
    FakeWebSocket.last().drop(); // refused before opening (e.g. expired login)
    expect(onRejected).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1000);
    FakeWebSocket.last().open();
    FakeWebSocket.last().drop(1008); // access changed
    expect(onRejected).toHaveBeenCalledTimes(2);
    FakeWebSocket.last().drop(1001); // ordinary close: no re-check
    expect(onRejected).toHaveBeenCalledTimes(2);
  });

  it('stops for good: closes the socket and never reconnects', () => {
    const c = client();
    c.start();
    FakeWebSocket.last().open();
    c.stop();
    expect(FakeWebSocket.last().readyState).toBe(3);
    expect(c.currentStatus).toBe('stopped');
    vi.advanceTimersByTime(60_000);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it('stopping while a reconnect is pending cancels it', () => {
    const c = client();
    c.start();
    FakeWebSocket.last().drop();
    c.stop();
    vi.advanceTimersByTime(60_000);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it('reconnects right away when the browser comes back online', () => {
    const c = client({ baseDelayMs: 10_000 });
    c.start();
    FakeWebSocket.last().open();
    FakeWebSocket.last().drop();
    window.dispatchEvent(new Event('online'));
    expect(FakeWebSocket.instances).toHaveLength(2);
    c.stop();
  });
});

describe('gatewayUrl', () => {
  it('uses the page origin with ws or wss', () => {
    expect(gatewayUrl({ protocol: 'http:', host: 'localhost:5173' })).toBe('ws://localhost:5173/ws');
    expect(gatewayUrl({ protocol: 'https:', host: 'tutti.uni.lu' })).toBe('wss://tutti.uni.lu/ws');
  });
});
