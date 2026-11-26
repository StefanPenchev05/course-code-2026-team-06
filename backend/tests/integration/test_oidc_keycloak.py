#!/usr/bin/env python3
"""Opt-in real Keycloak + Go + PostgreSQL/Redis + Chromium smoke test.
Uses a fresh demo project/volume only. No existing .env is overwritten.
Requires frontend/npm ci and Playwright's Chromium, plus the backend test tools.
"""
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess

ROOT = Path(__file__).resolve().parents[3]


def main():
    # Refuse to touch an existing provider (even if it is another Tutti demo).
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 8180))
    project = 'tutti-oidc-browser-' + secrets.token_hex(6)
    env = os.environ.copy()
    env.update(COMPOSE_PROJECT_NAME=project, KEYCLOAK_ADMIN_USERNAME='synthetic-admin',
               KEYCLOAK_ADMIN_PASSWORD=secrets.token_hex(32),
               TUTTI_OIDC_CLIENT_SECRET=secrets.token_hex(32), TUTTI_TEST_KEYCLOAK='1')
    compose = ['docker', 'compose', '-f', 'compose.yaml', '-f', 'compose.sso.yaml']
    # Suppress provider output; neither rendered config nor tokens belong in reports.
    def docker(*args):
        result = subprocess.run(list(args), cwd=ROOT, env=env, capture_output=True)
        if result.returncode:
            raise RuntimeError('Disposable Keycloak lifecycle command failed; inspect Docker privately')
        return result.stdout
    try:
        docker(*compose, 'config', '--quiet')
        docker(*compose, 'up', '-d', '--wait', '--wait-timeout', '180', 'keycloak')
        subprocess.run(['bash', 'backend/tests/integration/test_identity.sh', '-race', '-run', '^TestOIDCKeycloakBrowser$'],
                       cwd=ROOT, env=env, check=True)
    finally:
        # Never use combined down -v: it also names the application DB volume.
        cid = docker(*compose, 'ps', '-a', '-q', 'keycloak').decode().strip()
        if cid:
            info = json.loads(docker('docker', 'inspect', cid))[0]
            assert info['Config']['Labels']['com.docker.compose.project'] == project
            volumes = [m['Name'] for m in info['Mounts'] if m['Type'] == 'volume' and m['Destination'] == '/opt/keycloak/data']
            docker(*compose, 'rm', '-s', '-f', 'keycloak')
            for volume in volumes:
                metadata = json.loads(docker('docker', 'volume', 'inspect', volume))[0]
                assert metadata['Labels']['com.docker.compose.project'] == project
                docker('docker', 'volume', 'rm', volume)
        network = project + '_default'
        result = subprocess.run(['docker', 'network', 'inspect', network], capture_output=True)
        if result.returncode == 0:
            metadata = json.loads(result.stdout)[0]
            assert metadata['Labels']['com.docker.compose.project'] == project and not metadata.get('Containers')
            docker('docker', 'network', 'rm', network)


if __name__ == '__main__':
    main()
