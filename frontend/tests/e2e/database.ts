import { execFileSync } from 'node:child_process';
import { E2E } from './env';

/** Trusted fixture provisioning only. UI mutations must use the real HTTP API.
 * SQL variables are quoted by psql via :'name'; never interpolate user strings.
 */
export function runSql(sql: string, variables: Record<string, string> = {}): string {
  if (!E2E.databaseURL) throw new Error('E2E_DATABASE_URL must target a disposable test database.');
  try {
    return execFileSync(
      'psql',
      ['-X', E2E.databaseURL, '-v', 'ON_ERROR_STOP=1', '-qAt', ...Object.entries(variables).map(([key, value]) => `--set=${key}=${value}`)],
      { input: sql, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] },
    ).trim();
  } catch (error) {
    // Do not propagate the command/cause: it contains the database URL/password.
    const stderr = (error as { stderr?: Buffer | string }).stderr?.toString().trim();
    // eslint-disable-next-line preserve-caught-error -- Original cause includes database credentials in the command.
    throw new Error(`Could not provision disposable E2E fixtures: ${stderr || 'psql failed'}`);
  }
}
