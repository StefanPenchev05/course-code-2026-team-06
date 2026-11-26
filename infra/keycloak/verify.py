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
    assert client["enabled"] is True
    assert client["protocol"] == "openid-connect"
    assert client["publicClient"] is False
    assert client["clientAuthenticatorType"] == "client-secret"
    assert client["standardFlowEnabled"] is True
    for flag in ("implicitFlowEnabled", "directAccessGrantsEnabled", "serviceAccountsEnabled", "fullScopeAllowed"):
        assert client[flag] is False, flag
    assert client["redirectUris"] == [CALLBACK]
    assert client["webOrigins"] == ["http://localhost:8080"]
    assert client["attributes"]["pkce.code.challenge.method"] == "S256"
    for grant in ("oauth2.device.authorization.grant.enabled", "oidc.ciba.grant.enabled",
                  "standard.token.exchange.enabled", "oauth2.jwt.authorization.grant.enabled"):
        assert client["attributes"].get(grant) == "false", grant
    assert sorted(client["defaultClientScopes"]) == ["email", "profile"]
    assert not client.get("optionalClientScopes"), "Unexpected optional scopes"


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


def endpoint_status(url):
    # No redirects or response bodies in reports (logout may return HTML).
    try:
        with urllib.request.build_opener(NoRedirect).open(url, timeout=15) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


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
    assert realm["resetPasswordAllowed"] is False
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
    for name, suffix in (("authorization_endpoint", "auth"), ("token_endpoint", "token"), ("jwks_uri", "certs"),
                         ("userinfo_endpoint", "userinfo"), ("end_session_endpoint", "logout")):
        assert discovery[name] == ISSUER + "/protocol/openid-connect/" + suffix, name
    assert "S256" in discovery["code_challenge_methods_supported"]
    assert {"openid", "profile", "email"}.issubset(discovery["scopes_supported"])
    assert "client_secret_basic" in discovery["token_endpoint_auth_methods_supported"]
    assert "RS256" in discovery["id_token_signing_alg_values_supported"]
    assert endpoint_status(discovery["userinfo_endpoint"]) == 401, "Userinfo must require a bearer token"
    assert endpoint_status(discovery["end_session_endpoint"]) in (200, 400), "Logout endpoint unreachable"
    status, error = request_json(discovery["token_endpoint"], {"grant_type": "authorization_code"})
    assert status in (400, 401) and error.get("error"), "Unauthenticated exchange not rejected"
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
    status, location = authorization(dict(query, code_challenge_method="plain"))
    assert status == 400 or urllib.parse.parse_qs(urllib.parse.urlsplit(location).query).get("error") == ["invalid_request"], "PKCE plain accepted"
    status, location = authorization(dict(query, response_type="token"))
    redirect = urllib.parse.urlsplit(location)
    response_error = urllib.parse.parse_qs(redirect.query or redirect.fragment).get("error")
    assert status == 400 or response_error in (["unauthorized_client"], ["unsupported_response_type"]), "Implicit response not rejected"
    print("PASS: discovery/issuer/auth/token/userinfo/JWKS/logout; scopes, RS256 and client authentication; invalid redirect, missing/plain PKCE and implicit flow rejected")
    if not args.admin:
        return
    required = ("KEYCLOAK_ADMIN_USERNAME", "KEYCLOAK_ADMIN_PASSWORD", "TUTTI_OIDC_CLIENT_SECRET")
    if any(not os.environ.get(key, "").strip() for key in required):
        parser.error("--admin requires nonempty local administrator credentials and client secret")
    credentials = {key: os.environ[key] for key in required}
    status, auth = request_json("http://localhost:8180/realms/master/protocol/openid-connect/token", {
        "grant_type": "password", "client_id": "admin-cli", "username": credentials["KEYCLOAK_ADMIN_USERNAME"],
        "password": credentials["KEYCLOAK_ADMIN_PASSWORD"],
    })
    assert status == 200, "Local administrator authentication failed"
    status, persisted_realm = request_json("http://localhost:8180/admin/realms/tutti-demo", token=auth["access_token"])
    assert status == 200 and persisted_realm["realm"] == "tutti-demo" and persisted_realm["enabled"] is True
    assert persisted_realm["registrationAllowed"] is False and persisted_realm["resetPasswordAllowed"] is False
    status, clients = request_json("http://localhost:8180/admin/realms/tutti-demo/clients?clientId=tutti-backend", token=auth["access_token"])
    assert status == 200 and len(clients) == 1
    status, persisted = request_json("http://localhost:8180/admin/realms/tutti-demo/clients/" + clients[0]["id"], token=auth["access_token"])
    assert status == 200
    check_client(persisted)
    status, secret = request_json("http://localhost:8180/admin/realms/tutti-demo/clients/" + clients[0]["id"] + "/client-secret", token=auth["access_token"])
    assert status == 200 and secret["value"] == credentials["TUTTI_OIDC_CLIENT_SECRET"], "Client secret differs; existing realms skip startup import"
    status, error = request_json(discovery["token_endpoint"], {
        "grant_type": "password", "client_id": "tutti-backend", "client_secret": credentials["TUTTI_OIDC_CLIENT_SECRET"],
        "username": "nonexistent-synthetic-user", "password": "not-a-user-credential",
    })
    assert status == 400 and error["error"] == "unauthorized_client", "Password grant not disabled"
    status, error = request_json(discovery["token_endpoint"], {
        "grant_type": "client_credentials", "client_id": "tutti-backend",
        "client_secret": credentials["TUTTI_OIDC_CLIENT_SECRET"],
    })
    assert status == 401 and error["error"] == "unauthorized_client", "Service account grant not disabled"
    exchange = {"grant_type": "authorization_code", "client_id": "tutti-backend",
                "client_secret": credentials["TUTTI_OIDC_CLIENT_SECRET"],
                "code": "synthetic-invalid-code", "redirect_uri": CALLBACK}
    status, error = request_json(discovery["token_endpoint"], exchange)
    assert status == 400 and error["error"] == "invalid_grant", "Configured client secret did not authenticate"
    status, error = request_json(discovery["token_endpoint"], dict(exchange, client_secret="synthetic-deliberately-wrong"))
    assert status == 401 and error["error"] in ("invalid_client", "unauthorized_client"), "Wrong secret not rejected at client authentication"
    print("PASS: persisted realm/client/scopes/grants, resolved secret authenticates; wrong secret, password and service account grants rejected")
    print("Admin-cli's master-realm maintenance grant is separate from the disabled tutti-backend password grant.")


if __name__ == "__main__":
    main()
