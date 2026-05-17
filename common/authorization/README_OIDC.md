# OIDC Authorization with Keycloak

This guide walks through configuring Cadence's OIDC authorizer against a Keycloak realm. The same steps apply, with minor naming changes, to other standards-compliant OpenID Connect providers (Auth0, Okta, Dex, etc.).

## What this gives you

- **Standards-compliant token verification.** Signature, audience (`aud`), issuer (`iss`), and expiry (`exp`) are all validated against the provider's published JWKS. JWKS rotation is handled automatically.
- **Per-domain authorization.** Cadence groups (read / write / process) are mapped from a token claim and matched against the per-domain ACL stored in domain data.
- **Three rollout modes per domain.** `enabled` enforces auth, `shadow` logs would-have-denied requests but lets them through, `disabled` is a no-op. Switchable at runtime via dynamic config — no restart.
- **Separate admin auth path.** Admin-permission requests are gated by a separate dynamic config key.

## Try it out with Docker (5 minutes)

Two compose files are provided:

```bash
# Uses the published ubercadence/server image.
docker compose -f docker/docker-compose-oidc.yml up

# Builds the Cadence server from the local checkout. Use this if you've made
# changes to the OIDC code that aren't in a published image yet.
docker compose -f docker/docker-compose-oidc-dev.yml up
```

Both bring up Cassandra, Cadence (with OIDC enabled and pointed at the bundled Keycloak), Keycloak (with a pre-loaded `cadence` realm), and cadence-web.

The realm import (at `docker/keycloak/cadence-realm.json`) creates:
- Client `cadence-server` with an audience mapper so `aud` matches what Cadence verifies
- Realm roles `cadence-read`, `cadence-write`, `cadence-process`, `cadence-admin`
- Three test users, all with password `password`:
  - `alice` — has `cadence-read` + `cadence-write`
  - `bob-worker` — has `cadence-process`
  - `admin-user` — has the `cadence_admin` claim true (bypasses per-domain checks)

Once the stack is up:

```bash
Once the stack is up, the snippet below registers two domains with **different** group ACLs, then shows the same token succeeding on one and being denied on the other. This is the per-domain authorization story end to end.

```bash
# 1. Register two domains with different group ACLs.
#    alice-domain grants read/write to anyone with cadence-read or cadence-write roles.
#    bob-domain grants read only to anyone with the cadence-process role.
docker compose -f docker/docker-compose-oidc.yml exec cadence sh -c '
  cadence --do alice-domain domain register --gd false &&
  cadence --do alice-domain domain update \
    --domain_data READ_GROUPS=cadence-read,WRITE_GROUPS=cadence-write &&
  cadence --do bob-domain domain register --gd false &&
  cadence --do bob-domain domain update \
    --domain_data READ_GROUPS=cadence-process'

# 2. Turn enforcement on for both domains via dynamic config.
docker compose -f docker/docker-compose-oidc.yml exec cadence sh -c \
  'printf "\nsystem.enableAuthorizationV2:\n  - value: \"enabled\"\n    constraints:\n      domainName: \"alice-domain\"\n  - value: \"enabled\"\n    constraints:\n      domainName: \"bob-domain\"\n" >> /etc/cadence/config/dynamicconfig/development.yaml'
docker compose -f docker/docker-compose-oidc.yml restart cadence
# wait ~30s for the frontend to come back

# 3. Grab tokens for two users. The realm preload gives alice cadence-read+write,
#    and bob-worker cadence-process — so each user has access to exactly one domain.
ALICE_TOKEN=$(curl -s -X POST http://localhost:8080/realms/cadence/protocol/openid-connect/token \
  -d grant_type=password -d client_id=cadence-server \
  -d username=alice -d password=password | jq -r .access_token)

BOB_TOKEN=$(curl -s -X POST http://localhost:8080/realms/cadence/protocol/openid-connect/token \
  -d grant_type=password -d client_id=cadence-server \
  -d username=bob-worker -d password=password | jq -r .access_token)

# 4. alice succeeds on alice-domain, fails on bob-domain.
docker compose -f docker/docker-compose-oidc.yml exec cadence \
  cadence --do alice-domain --jwt "$ALICE_TOKEN" workflow list   # → empty list, no error
docker compose -f docker/docker-compose-oidc.yml exec cadence \
  cadence --do bob-domain --jwt "$ALICE_TOKEN" workflow list     # → Request unauthorized

# 5. bob has the inverse pattern — succeeds on bob-domain, fails on alice-domain.
docker compose -f docker/docker-compose-oidc.yml exec cadence \
  cadence --do bob-domain --jwt "$BOB_TOKEN" workflow list       # → empty list, no error
docker compose -f docker/docker-compose-oidc.yml exec cadence \
  cadence --do alice-domain --jwt "$BOB_TOKEN" workflow list     # → Request unauthorized
```

Both tokens verify equally well (same signature, same issuer, same audience). The difference is the per-domain group check inside `validatePermission` — alice's `cadence-read` role isn't in `bob-domain`'s `READ_GROUPS`, so the call is denied even though authentication passed. This is the authn-vs-authz distinction Cadence's authorizer interface bundles into a single decision.

> **Note on the `iss` claim.** The compose file sets Keycloak's `--hostname=http://keycloak:8080` so tokens are minted with `iss=http://keycloak:8080/realms/cadence`. This matches Cadence's `OIDC_ISSUER_URL` (it talks to Keycloak via the docker DNS name `keycloak`). You curl Keycloak via the forwarded port `localhost:8080` from the host — that still works because `--hostname-strict=false` is set, but the `iss` claim in the resulting token is the docker-network value, not `localhost`.

### Using the cadence-web UI

The bundled cadence-web container is started with `CADENCE_WEB_AUTH_STRATEGY=jwt`, which enables the existing **Login with JWT** menu item in the top-right user menu:

1. Open <http://localhost:8088>.
2. Click the user icon in the nav bar → **Login with JWT**.
3. Paste the `$TOKEN` from step 3 above.
4. The UI now sends that token as `cadence-authorization` gRPC metadata on every request to the backend.

The token is stored as an HttpOnly cookie called `cadence-authorization` and is forwarded server-side by cadence-web — it is never visible to client-side JavaScript. Logging out clears the cookie. When the token expires, the UI starts seeing authorization errors and you re-paste a fresh one.

This is a manual paste flow because cadence-web does not yet implement an OIDC redirect. Adding a "Login with Keycloak" button that does PKCE end-to-end is tracked as a follow-up in the cadence-web repo; it would set the same cookie via the same `POST /api/auth/token` endpoint, so no backend changes are required when it lands.

The Keycloak admin console is at <http://localhost:8080> (admin / admin) — useful for inspecting the realm, adjusting user roles, or copy-pasting into the manual setup below.

## Quick-start with Keycloak (manual)

These steps assume Keycloak 24+ on the standard admin console. Use this section if you're configuring an existing Keycloak instance rather than running the bundled Docker setup.

### 1. Create a realm

```
Realm name:  cadence
```

### 2. Create a client

```
Client ID:                 cadence-server
Client type:               OpenID Connect
Client authentication:     On  (or Off — Cadence only verifies tokens, it does not initiate flows)
Authentication flow:       Standard flow + Direct access grants
Valid redirect URIs:       *  (or specific values for your CLI/UI)
```

### 3. Add an audience mapper

By default Keycloak does not include the client ID in the `aud` claim. Cadence verifies `aud` against the configured `clientID`, so you need to add a mapper:

```
Clients → cadence-server → Client scopes → cadence-server-dedicated → Add mapper → By configuration → Audience
  Name:                     cadence-aud
  Included Client Audience: cadence-server
  Add to ID token:          On
  Add to access token:      On
```

### 4. Define realm roles

```
Realm roles → Create role:
  - cadence-read
  - cadence-write
  - cadence-process
  - cadence-admin   (optional — used for the admin claim)
```

Assign roles to users via **Users → <user> → Role mapping → Assign role**.

### 5. (Optional) Add an admin claim

If you want certain users to bypass per-domain checks entirely, add a claim that resolves to a boolean. The simplest path is a hardcoded mapper that's only added to a specific role:

```
Clients → cadence-server → Client scopes → cadence-server-dedicated → Add mapper → By configuration → User Attribute
  Name:                cadence-admin
  User Attribute:      cadence_admin
  Token Claim Name:    cadence_admin
  Claim JSON Type:     boolean
```

Then set `cadence_admin = true` on individual user attributes.

Alternatively, use a **Hardcoded claim** mapper conditioned on role membership.

### 6. Configure Cadence

Add the following block to your server YAML config:

```yaml
authorization:
  oidcAuthorizer:
    enable: true
    issuerURL: "https://keycloak.example.com/realms/cadence"
    clientID:  "cadence-server"
    # JMESPath: flatten the realm_access.roles array into a space-separated string
    # so it lines up with how Cadence groups are matched.
    groupsAttributePath: "realm_access.roles | join(' ', @)"
    adminAttributePath:  "cadence_admin"
    maxJwtTTL: 3600   # reject tokens whose remaining lifetime exceeds 1h
```

### 7. Map Keycloak roles to Cadence domain ACLs

For each domain you want to gate, set the group ACLs in the domain's data:

```bash
cadence --do my-domain domain update \
  --domain_data READ_GROUPS=cadence-read,WRITE_GROUPS=cadence-write,PROCESS_GROUPS=cadence-process
```

A user whose token contains any of those group names in `realm_access.roles` will be granted the matching permission level. The admin claim, if true, bypasses these checks entirely.

### 8. Send tokens

Cadence reads the bearer token from the gRPC metadata header `cadence-authorization`. Most clients have a flag for this:

```bash
TOKEN=$(curl -s -X POST \
  -d 'grant_type=password' \
  -d 'client_id=cadence-server' \
  -d 'client_secret=<secret>' \
  -d 'username=alice' \
  -d 'password=<password>' \
  https://keycloak.example.com/realms/cadence/protocol/openid-connect/token | jq -r .access_token)

cadence --address 127.0.0.1:7933 \
        --jwt "$TOKEN" \
        --do my-domain workflow list
```

## Rollout playbook (modes)

The OIDC authorizer reads two dynamic config keys:

| Key | Scope | Controls |
|---|---|---|
| `system.enableAuthorizationV2` | per-domain | mode for non-admin requests against that domain |
| `system.enableAdminAuthorization` | global | mode for admin-permission requests |

Each accepts `disabled`, `shadow`, or `enabled`. Default: `disabled`.

A safe rollout looks like:

```
1. Deploy with oidcAuthorizer.enable: true and both keys defaulting to "disabled"
   → No requests are blocked; nothing changes operationally.

2. For one canary domain, set system.enableAuthorizationV2 → "shadow"
   → Cadence verifies every request and logs a "would have denied" warning if it
     would have been rejected, but still allows the request. Watch the logs.

3. Once shadow mode is quiet for that domain, flip to "enabled"
   → Real enforcement begins.

4. Repeat per domain.

5. Flip system.enableAdminAuthorization separately for cluster-admin operations.
```

If you ever need to disable enforcement quickly without rolling code, set the relevant key back to `disabled` — it takes effect on the next dynamic config refresh.

## Operational notes

- **Discovery happens at startup.** The server contacts `<issuerURL>/.well-known/openid-configuration` and caches the JWKS endpoint. If discovery fails, the server fails to boot — make sure the OIDC provider is reachable before starting Cadence (in compose setups, gate `cadence` on a readiness check of the provider).
- **JWKS rotation is automatic.** The `go-oidc` `RemoteKeySet` refreshes keys in the background when verification encounters an unknown `kid`.
- **`maxJwtTTL` protects against long-lived tokens.** Even if the OIDC provider issues a token with `exp` 30 days out, Cadence rejects it if `(exp - now)` exceeds this ceiling.
- **Errors are mapped to deny, not surfaced.** Per the existing convention, signature/audience/issuer/expiry/group failures all become `DecisionDeny` with no error returned to the caller. Inspect server debug logs for the underlying reason. Domain cache failures are surfaced as gRPC errors.

## Sample decoded Keycloak ID token

```json
{
  "exp": 1735689600,
  "iat": 1735686000,
  "iss": "https://keycloak.example.com/realms/cadence",
  "aud": "cadence-server",
  "sub": "f:73a3...:alice",
  "typ": "Bearer",
  "preferred_username": "alice",
  "realm_access": {
    "roles": ["cadence-read", "cadence-write"]
  },
  "cadence_admin": false
}
```

With the YAML config from step 6, this token would be:

- Verified against the realm's JWKS published at `<iss>/protocol/openid-connect/certs`
- Accepted because `aud` matches `cadence-server` and `exp - now < 3600`
- Granted whichever of read/write/process matches the requested permission for the target domain (assuming the domain's `READ_GROUPS` / `WRITE_GROUPS` / `PROCESS_GROUPS` include either `cadence-read` or `cadence-write`)

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Server fails to boot with `OIDC discovery: ...` | `issuerURL` wrong, Keycloak unreachable, or TLS misconfigured |
| All requests get `DecisionDeny` even with a valid-looking token | Check `aud` claim — Keycloak omits the client ID by default; add the audience mapper from step 3 |
| `extracting groups claim: ... resolved to []interface {}, expected string` | `groupsAttributePath` must produce a string. Use `join(' ', @)` to flatten arrays. |
| Token rejected with `token TTL ... exceeds configured maximum` | Either lower the token lifetime in Keycloak, or raise `maxJwtTTL` in the Cadence config |
| Per-domain mode key has no effect | Confirm `Filters: [DomainName]` is set on `EnableAuthorizationV2` (it is by default in this build) and that the dynamic config source is actually being read |

## Choosing between `oauthAuthorizer` and `oidcAuthorizer`

Both can verify JWTs. Use `oidcAuthorizer` if your provider supports OIDC discovery (most do — Keycloak, Auth0, Okta, Dex, Google, etc.) — it gives you audience/issuer validation, automatic JWKS rotation, and the rollout modes documented above. Stick with `oauthAuthorizer` only if you have an existing static-JWKS or static-RSA-public-key deployment that you don't want to change.

The two are mutually exclusive — enable at most one in the YAML.
