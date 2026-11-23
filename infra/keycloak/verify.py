#!/usr/bin/env python3
"""Validate the demo import; optionally inspect a running provider (no Go login)."""
import argparse
import json
import os
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request

ISSUER = "http://localhost:8180/realms/tutti-demo"
CALLBACK = "http://localhost:8080/api/v1/auth/oidc/callback"


def check_client(client):
    assert client["clientId"] == "tutti-backend"
    assert client["protocol"] == "openid-connect"
    assert client["publicClient"] is False
    assert client["clientAuthenticatorType"] == "client-secret"
    assert client["standardFlowEnabled"] is True
    for flag in ("implicitFlowEnabled", "directAccessGrantsEnabled", "serviceAccountsEnabled", "fullScopeAllowed"):
        assert client[flag] is False, flag
    assert client["redirectUris"] == [CALLBACK]
    assert client["webOrigins"] == ["http://localhost:8080"]
    assert client["attributes"]["pkce.code.challenge.method"] == "S256"


def request_json(url, data=None, token=None):
    headers = {"Authorization": "Bearer " + token} if token else {}
    if data is not None:
        data = urllib.parse.urlencode(data).encode()
    req = urllib.request.Request(url, data=data, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        # Deliberately do not print response bodies, tokens or credentials.
        return error.code, json.loads(error.read())


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def authorization(query):
    url = ISSUER + "/protocol/openid-connect/auth?" + urllib.parse.urlencode(query)
    opener = urllib.request.build_opener(NoRedirect)
    try:
        with opener.open(url, timeout=15) as response:
            return response.status, response.headers.get("Location", "")
    except urllib.error.HTTPError as error:
        return error.code, error.headers.get("Location", "")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true", help="check discovery, keys and authorization restrictions")
    parser.add_argument("--admin", action="store_true", help="also verify persisted client using local admin env credentials")
    args = parser.parse_args()
    realm = json.loads((Path(__file__).parent / "realm/tutti-demo-realm.json").read_text())
    assert realm["realm"] == "tutti-demo" and realm["enabled"] is True
    assert realm["registrationAllowed"] is False
    assert not realm.get("users"), "Never commit user credentials"
    client = realm["clients"][0]
    assert len(realm["clients"]) == 1
    check_client(client)
    assert client["secret"] == "${TUTTI_OIDC_CLIENT_SECRET}"
    print("PASS: realm/client import, exact callback/origin, confidential code flow, S256; no seeded users/secrets")
    if not (args.live or args.admin):
        return
    status, discovery = request_json(ISSUER + "/.well-known/openid-configuration")
    assert status == 200 and discovery["issuer"] == ISSUER
    for name, suffix in (("authorization_endpoint", "auth"), ("token_endpoint", "token"), ("jwks_uri", "certs")):
        assert discovery[name] == ISSUER + "/protocol/openid-connect/" + suffix, name
    assert "S256" in discovery["code_challenge_methods_supported"]
    status, keys = request_json(discovery["jwks_uri"])
    assert status == 200 and keys["keys"]
    query = dict(client_id="tutti-backend", response_type="code", scope="openid", redirect_uri=CALLBACK,
                 state="synthetic-verification-state", nonce="synthetic-verification-nonce",
                 code_challenge="a" * 43, code_challenge_method="S256", prompt="none")
    status, location = authorization(query)
    assert status in (302, 303) and location.startswith(CALLBACK + "?")
    assert urllib.parse.parse_qs(urllib.parse.urlsplit(location).query).get("error") == ["login_required"]
    status, location = authorization(dict(query, redirect_uri="http://localhost:8080/unapproved"))
    assert status == 400 and not location, "Unexpected redirect URI accepted"
    missing_pkce = {k: v for k, v in query.items() if not k.startswith("code_challenge")}
    status, location = authorization(missing_pkce)
    assert status == 400 or urllib.parse.parse_qs(urllib.parse.urlsplit(location).query).get("error") == ["invalid_request"]
    status, location = authorization(dict(query, response_type="token"))
    redirect = urllib.parse.urlsplit(location)
    response_error = urllib.parse.parse_qs(redirect.query or redirect.fragment).get("error")
    assert status == 400 or response_error in (["unauthorized_client"], ["unsupported_response_type"]), "Implicit response not rejected"
    print("PASS: live discovery/issuer/endpoints/JWKS; valid code-flow preflight; invalid redirect, missing PKCE and implicit flow rejected")
    if not args.admin:
        return
    credentials = {key: os.environ[key] for key in ("KEYCLOAK_ADMIN_USERNAME", "KEYCLOAK_ADMIN_PASSWORD", "TUTTI_OIDC_CLIENT_SECRET")}
    status, auth = request_json("http://localhost:8180/realms/master/protocol/openid-connect/token", {
        "grant_type": "password", "client_id": "admin-cli", "username": credentials["KEYCLOAK_ADMIN_USERNAME"],
        "password": credentials["KEYCLOAK_ADMIN_PASSWORD"],
    })
    assert status == 200, "Local administrator authentication failed"
    status, clients = request_json("http://localhost:8180/admin/realms/tutti-demo/clients?clientId=tutti-backend", token=auth["access_token"])
    assert status == 200 and len(clients) == 1
    check_client(clients[0])
    status, secret = request_json("http://localhost:8180/admin/realms/tutti-demo/clients/" + clients[0]["id"] + "/client-secret", token=auth["access_token"])
    assert status == 200 and secret["value"] == credentials["TUTTI_OIDC_CLIENT_SECRET"], "Client secret differs; existing realms skip startup import"
    status, error = request_json(discovery["token_endpoint"], {
        "grant_type": "password", "client_id": "tutti-backend", "client_secret": credentials["TUTTI_OIDC_CLIENT_SECRET"],
        "username": "nonexistent-synthetic-user", "password": "not-a-user-credential",
    })
    assert status == 400 and error["error"] == "unauthorized_client", "Password grant not disabled"
    print("PASS: persisted client flags/PKCE/allowlists, injected secret and disabled password grant")
    print("Admin-cli's master-realm maintenance grant is separate from the disabled tutti-backend password grant.")


if __name__ == "__main__":
    main()
