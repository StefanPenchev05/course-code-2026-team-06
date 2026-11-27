/// <reference types="vite/client" />

interface ImportMetaEnv {
    /** Development only: "false" hides the test-account password form (see features/auth/devLogin.ts). */
    readonly VITE_DEV_PASSWORD_LOGIN?: string;
  }