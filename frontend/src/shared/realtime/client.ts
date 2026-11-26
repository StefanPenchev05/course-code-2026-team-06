import { parseEvent, RecentIds, type EventType, type RealtimeEvent } from './protocol';

/**
 * The app's single connection to the authenticated `/ws` route (docs/realtime.md).
 *
 * - One socket per client; `start()` twice never opens a second one.
 * - Never sends application messages (the server closes the socket if it gets one).
 * - Reconnects after any close with bounded exponential backoff plus jitter,
 *   immediately when the browser comes back online, and never after `stop()`.
 * - Ignores malformed, other-version and repeated messages.
 * - Calls `onConnected` after every successful (re)connection so views refetch what
 *   they may have missed: there is no replay of events sent while disconnected.
 */

export type ConnectionStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'stopped';

export type EventListener = (event: RealtimeEvent | { type: 'unknown'; id: string }) => void;

export interface RealtimeClientOptions {
  /** Absolute ws:// or wss:// URL of the gateway. */
  url: string;
  /** Called after each successful open (1st, 2nd, …): refetch visible data. */
  onConnected?: (connectionNumber: number) => void;
  /**
   * Called when a connection attempt failed before opening, or the server closed with
   * 1008 (access or policy changed). The app re-checks the session and calls `stop()` if
   * the user is no longer allowed in, so a rejected login doesn't reconnect forever.
   */
  onRejected?: () => void;
  onStatus?: (status: ConnectionStatus) => void;
  /** Injected for tests. */
  WebSocketImpl?: typeof WebSocket;
  random?: () => number;
  baseDelayMs?: number;
  maxDelayMs?: number;
}

const POLICY_VIOLATION = 1008;

export class RealtimeClient {
  private socket: WebSocket | null = null;
  private status: ConnectionStatus = 'idle';
  private attempt = 0;
  private connections = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private readonly listeners = new Map<EventType | '*', Set<EventListener>>();
  private readonly recent = new RecentIds();
  private readonly options: Required<Omit<RealtimeClientOptions, 'onConnected' | 'onRejected' | 'onStatus'>> &
    Pick<RealtimeClientOptions, 'onConnected' | 'onRejected' | 'onStatus'>;

  constructor(options: RealtimeClientOptions) {
    this.options = {
      WebSocketImpl: globalThis.WebSocket,
      random: Math.random,
      baseDelayMs: 1000,
      maxDelayMs: 30_000,
      ...options,
    };
  }

  get currentStatus(): ConnectionStatus {
    return this.status;
  }

  start(): void {
    if (this.status !== 'idle' && this.status !== 'stopped') return;
    if (typeof window !== 'undefined') window.addEventListener('online', this.onOnline);
    this.connect();
  }

  /** Closes the socket and stops reconnecting (logout, lost access, unmount). */
  stop(): void {
    if (typeof window !== 'undefined') window.removeEventListener('online', this.onOnline);
    this.clearTimer();
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null;
      if (socket.readyState === socket.CONNECTING || socket.readyState === socket.OPEN) socket.close(1000);
    }
    this.setStatus('stopped');
  }

  /** Listens to one event type, or '*' for every valid event. Returns the unsubscribe function. */
  subscribe(type: EventType | '*', listener: EventListener): () => void {
    let set = this.listeners.get(type);
    if (!set) {
      set = new Set();
      this.listeners.set(type, set);
    }
    set.add(listener);
    return () => {
      set.delete(listener);
    };
  }

  private connect(): void {
    this.clearTimer();
    this.setStatus(this.connections === 0 ? 'connecting' : 'reconnecting');
    let opened = false;
    let socket: WebSocket;
    try {
      socket = new this.options.WebSocketImpl(this.options.url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.socket = socket;

    socket.onopen = () => {
      opened = true;
      this.attempt = 0;
      this.connections += 1;
      this.setStatus('open');
      this.options.onConnected?.(this.connections);
    };
    socket.onmessage = (message: MessageEvent) => this.dispatch(message.data);
    socket.onclose = (close: CloseEvent) => {
      if (this.socket !== socket) return;
      this.socket = null;
      // Rejected handshake (no open) or closed for access/policy reasons: let the app re-check the session.
      if (!opened || close.code === POLICY_VIOLATION) this.options.onRejected?.();
      if (this.status !== 'stopped') this.scheduleReconnect();
    };
    socket.onerror = () => {
      // A close event always follows; reconnecting is handled there.
    };
  }

  private dispatch(data: unknown): void {
    const event = parseEvent(data);
    if (!event || !this.recent.add(event.id)) return;
    const typed = event.type === 'unknown' ? { type: 'unknown' as const, id: event.id } : event;
    const targets = [...(event.type === 'unknown' ? [] : (this.listeners.get(event.type) ?? [])), ...(this.listeners.get('*') ?? [])];
    for (const listener of targets) {
      try {
        listener(typed);
      } catch (error) {
        // One broken listener must not stop the others or the connection.
        console.error('Realtime listener failed', error);
      }
    }
  }

  /** Delay = random share (50–100 %) of min(max, base × 2^attempt). */
  nextDelay(): number {
    const ceiling = Math.min(this.options.maxDelayMs, this.options.baseDelayMs * 2 ** this.attempt);
    return Math.round(ceiling * (0.5 + this.options.random() * 0.5));
  }

  private scheduleReconnect(): void {
    if (this.status === 'stopped') return;
    this.setStatus('reconnecting');
    const delay = this.nextDelay();
    this.attempt += 1;
    this.timer = setTimeout(() => this.connect(), delay);
  }

  private readonly onOnline = () => {
    // Back online: try right away instead of waiting out the backoff.
    if (this.status === 'reconnecting' && !this.socket) {
      this.attempt = 0;
      this.connect();
    }
  };

  private clearTimer(): void {
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
  }

  private setStatus(status: ConnectionStatus): void {
    if (this.status === status) return;
    this.status = status;
    this.options.onStatus?.(status);
  }
}

/** The same-origin gateway URL: ws:// for http pages, wss:// for https pages. */
export function gatewayUrl(location: Pick<Location, 'protocol' | 'host'> = window.location): string {
  return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws`;
}
