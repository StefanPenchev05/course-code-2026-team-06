import { createContext, useContext } from 'react';
import type { ConnectionStatus, RealtimeClient } from './client';

export interface RealtimeState {
  /** The app's single client while a user is logged in; null otherwise. */
  client: RealtimeClient | null;
  status: ConnectionStatus;
  /** Increases on every successful (re)connection; views refetch when it changes. */
  connectionNumber: number;
}

export const RealtimeContext = createContext<RealtimeState>({ client: null, status: 'idle', connectionNumber: 0 });

export function useRealtime(): RealtimeState {
  return useContext(RealtimeContext);
}
