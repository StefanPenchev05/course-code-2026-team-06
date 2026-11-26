/**
 * Version-1 realtime envelope (docs/realtime.md "Exact wire format").
 * Messages only say THAT something changed; the app always refetches the
 * authoritative data over HTTP and never treats a message as data.
 */

export const EVENT_TYPES = [
  'booking.changed',
  'availability.changed',
  'tutor_application.changed',
  'coordination.changed',
  'activity.changed',
  'account.access_changed',
] as const;

export type EventType = (typeof EVENT_TYPES)[number];

export interface RealtimeEvent {
  version: 1;
  id: string;
  type: EventType;
  /** Session, tutor, application or case UUID; absent for account.access_changed. */
  entityId?: string;
  occurredAt: string;
}

/** A well-formed version-1 message of a type this client doesn't know yet. */
export interface UnknownEvent {
  version: 1;
  id: string;
  type: 'unknown';
}

export const MAX_FRAME_BYTES = 1024;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const NIL_UUID = '00000000-0000-0000-0000-000000000000';
const RFC3339_UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$/;
const KEYS = new Set(['version', 'id', 'type', 'entityId', 'occurredAt']);

const isUuid = (value: unknown): value is string => typeof value === 'string' && UUID.test(value) && value !== NIL_UUID;

/**
 * Parses one text frame. Returns null for anything malformed, oversized or of
 * another version (ignored, never thrown), an UnknownEvent for a valid frame with
 * an unknown type, or the typed event.
 */
export function parseEvent(frame: unknown): RealtimeEvent | UnknownEvent | null {
  if (typeof frame !== 'string' || new TextEncoder().encode(frame).length > MAX_FRAME_BYTES) return null;
  let value: unknown;
  try {
    value = JSON.parse(frame);
  } catch {
    return null;
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  if (Object.keys(record).some((key) => !KEYS.has(key))) return null;
  if (record.version !== 1 || !isUuid(record.id) || typeof record.type !== 'string') return null;
  if (typeof record.occurredAt !== 'string' || !RFC3339_UTC.test(record.occurredAt)) return null;

  if (!(EVENT_TYPES as readonly string[]).includes(record.type)) return { version: 1, id: record.id, type: 'unknown' };
  const type = record.type as EventType;
  if (type === 'account.access_changed') {
    if ('entityId' in record) return null;
  } else if (!isUuid(record.entityId)) {
    return null;
  }
  return { version: 1, id: record.id, type, entityId: record.entityId as string | undefined, occurredAt: record.occurredAt };
}

/** Remembers the last `size` event IDs so a repeated delivery is handled once. */
export class RecentIds {
  private readonly seen = new Set<string>();
  constructor(private readonly size = 512) {}

  /** True the first time an ID is seen, false for a repeat. */
  add(id: string): boolean {
    if (this.seen.has(id)) return false;
    this.seen.add(id);
    if (this.seen.size > this.size) this.seen.delete(this.seen.values().next().value as string);
    return true;
  }
}
