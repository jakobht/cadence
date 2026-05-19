# OIDC Authorization with Keycloak

This guide walks through configuring Cadence's OIDC authorizer against a Keycloak realm. The same steps apply, with minor naming changes, to other standards-compliant OpenID Connect providers (Auth0, Okta, Dex, etc.).

## What this gives you

- **Standards-compliant token verification.** Signature, audience (`aud`), issuer (`iss`), and expiry (`exp`) are all validated against the provider's published JWKS. JWKS rotation is handled automatically.
- **Role-driven authorization, source of truth in Keycloak.** A user is allowed an operation when their token contains a role of the form `cadence/{read|write|process|admin}[/{domain}]` granting the requested permission. No Cadence-side ACL setup; all permissions live in Keycloak.
- **Per-domain restriction via role naming.** `cadence/write` grants write on every domain; `cadence/write/alice-domain` grants write only on `alice-domain`.
- **Admin bypass.** The role `cadence/admin` (or a token claim resolving to `true` via `adminAttributePath`) bypasses all per-permission checks.
- **Three rollout modes per domain.** `enabled` enforces auth, `shadow` logs would-have-denied requests but lets them through, `disabled` is a no-op. Switchable at runtime via dynamic config — no restart.
- **Separate admin auth path.** Admin-permission requests are gated by a separate dynamic config key.

## Try it out with Docker (5 minutes)

**One-time host setup:** add `127.0.0.1 keycloak` to your `/etc/hosts` so your browser can resolve the same hostname that's in the tokens' `iss` claim. Without this, the curl / CLI flow works (Keycloak accepts requests on any hostname) but the admin UI breaks, because the admin SPA redirects to `http://keycloak:8080/realms/master/...` for login and your browser won't know that name.

```bash
echo '127.0.0.1 keycloak' | sudo tee -a /etc/hosts
```

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
- Realm roles:
  - global: `cadence/read`, `cadence/write`, `cadence/process`, `cadence/admin`
  - domain-scoped: `cadence/read/alice-domain`, `cadence/write/alice-domain`, `cadence/read/bob-domain`, `cadence/write/bob-domain`, `cadence/process/bob-domain`
- Three test users, all with password `password`:
  - `alice` → roles `cadence/{read,write}/alice-domain` (read+write alice-domain only)
  - `bob-worker` → roles `cadence/{read,write,process}/bob-domain` (read+write+poll bob-domain only)
  - `admin-user` → role `cadence/admin` + `cadence_admin: true` claim (bypass)

### Walkthrough — same token allowed on one domain, denied on another, then a live role change

Auth enforcement is already on for every domain — the bundled compose appends
`docker/oidc-dynamicconfig-extras.yaml` (`system.enableAuthorizationV2: enabled`)
to the image's `development.yaml` on startup, so no manual dynamic-config setup
is required.

```bash
# 1. Register two demo domains (no ACL setup needed — auth lives in Keycloak roles).
./cadence --do alice-domain domain register --gd false
./cadence --do bob-domain   domain register --gd false

# 2. Get alice's token.
TOKEN=$(curl -s -X POST http://localhost:8080/realms/cadence/protocol/openid-connect/token \
  -d grant_type=password -d client_id=cadence-server \
  -d username=alice -d password=password | jq -r .access_token)

# 3. alice → alice-domain ✓  (token has cadence/{read,write}/alice-domain)
./cadence --do alice-domain --jwt "$TOKEN" workflow list

# 4. alice → bob-domain ✗  (no role for bob-domain in her token)
./cadence --do bob-domain   --jwt "$TOKEN" workflow list

# 5. 🪄 Grant alice cadence/read/bob-domain in Keycloak.
#    UI: http://keycloak:8080/admin/  (admin / admin) → realm "cadence" → Users →
#        alice → Role mapping → Assign role → cadence/read/bob-domain
#    REST equivalent:
ADMIN=$(curl -s -X POST http://localhost:8080/realms/master/protocol/openid-connect/token \
  -d grant_type=password -d client_id=admin-cli \
  -d username=admin -d password=admin | jq -r .access_token)
ALICE_ID=$(curl -s -H "Authorization: Bearer $ADMIN" \
  "http://localhost:8080/admin/realms/cadence/users?username=alice" | jq -r '.[0].id')
ROLE=$(curl -s -H "Authorization: Bearer $ADMIN" \
  "http://localhost:8080/admin/realms/cadence/roles/cadence%2Fread%2Fbob-domain")
curl -s -X POST -H "Authorization: Bearer $ADMIN" \
  -H "Content-Type: application/json" -d "[$ROLE]" \
  "http://localhost:8080/admin/realms/cadence/users/$ALICE_ID/role-mappings/realm"

# 6. Old token still fails — JWTs are signed snapshots, Keycloak changes don't
#    retroactively rewrite tokens already in clients' hands.
./cadence --do bob-domain --jwt "$TOKEN" workflow list      # ✗

# 7. New token works — re-auth picks up the new role.
NEW_TOKEN=$(curl -s -X POST http://localhost:8080/realms/cadence/protocol/openid-connect/token \
  -d grant_type=password -d client_id=cadence-server \
  -d username=alice -d password=password | jq -r .access_token)
./cadence --do bob-domain --jwt "$NEW_TOKEN" workflow list  # ✓
```

### Using the cadence-web UI

The bundled cadence-web container is started with `CADENCE_WEB_AUTH_STRATEGY=jwt`, which enables the existing **Login with JWT** menu item in the top-right user menu:

1. Open <http://localhost:8088>.
2. Click the user icon in the nav bar → **Login with JWT**.
3. Paste a token (e.g. `$TOKEN` from step 3 above).
4. The UI now sends that token as `cadence-authorization` gRPC metadata on every request to the backend.

The token is stored as an HttpOnly cookie called `cadence-authorization` and is forwarded server-side by cadence-web — it is never visible to client-side JavaScript. Logging out clears the cookie. When the token expires, the UI starts seeing authorization errors and you re-paste a fresh one.

This is a manual paste flow because cadence-web does not yet implement an OIDC redirect. Adding a "Login with Keycloak" button that does PKCE end-to-end is tracked as a follow-up in the cadence-web repo; it would set the same cookie via the same `POST /api/auth/token` endpoint, so no backend changes are required when it lands.

The Keycloak admin console is at <http://keycloak:8080/admin/> (admin / admin) — useful for inspecting the realm, adjusting user roles, or copy-pasting into the manual setup below.

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

Create one role per permission/domain combination you want to grant. Convention: `cadence/{read|write|process|admin}[/{domain-name}]`. Examples:

```
Realm roles → Create role:
  - cadence/read                       # read any domain
  - cadence/write                      # write any domain
  - cadence/process                    # poll any domain (workers)
  - cadence/admin                      # admin bypass
  - cadence/write/prod-payments        # write only the "prod-payments" domain
  - cadence/read/prod-payments         # read only the "prod-payments" domain
```

Assign roles to users via **Users → <user> → Role mapping → Assign role**.

### 5. (Optional) Add an admin claim

If you want certain users to bypass everything via a boolean claim rather than the `cadence/admin` role, add a mapper:

```
Clients → cadence-server → Client scopes → cadence-server-dedicated → Add mapper → By configuration → User Attribute
  Name:                cadence-admin
  User Attribute:      cadence_admin
  Token Claim Name:    cadence_admin
  Claim JSON Type:     boolean
```

Then set `cadence_admin = true` on individual user attributes.

### 6. Configure Cadence

Add the following block to your server YAML config:

```yaml
authorization:
  oidcAuthorizer:
    enable: true
    issuerURL: "https://keycloak.example.com/realms/cadence"
    clientID:  "cadence-server"
    # JMESPath: flatten realm_access.roles into a space-separated string for parsing
    groupsAttributePath: "realm_access.roles | join(' ', @)"
    adminAttributePath:  "cadence_admin"
    maxJwtTTL: 3600   # reject tokens whose remaining lifetime exceeds 1h
```

### 7. Send tokens

Cadence reads the bearer token from the gRPC metadata header `cadence-authorization`. The OSS CLI sends it via the `--jwt` flag:

```bash
TOKEN=$(curl -s -X POST \
  -d 'grant_type=password' \
  -d 'client_id=cadence-server' \
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
- **Role changes are not retroactive.** Tokens are signed snapshots; revoking a role in Keycloak only affects tokens issued after the change. Effective revocation latency = token lifetime. Tune `accessTokenLifespan` accordingly.
- **Errors are mapped to deny.** Per the existing convention, signature/audience/issuer/expiry/role-mismatch failures all become `DecisionDeny` with no error returned to the caller. Inspect server logs for the underlying reason.
- **Sticky / ephemeral task-list polls bypass auth.** `PollForActivityTask` and `PollForDecisionTask` calls with `kind != Normal` are allowed without a token check. This exists so workers built against current SDK clients (which don't attach tokens to poll calls) keep working when OIDC is turned on. Sticky list names are server-generated and randomized, but a caller able to reach the frontend and guess a name could intercept tasks — guard the frontend at the network layer if your threat model includes that. The proper fix requires SDK changes to attach tokens on every poll.

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
    "roles": ["cadence/read/prod-payments", "cadence/write/prod-payments"]
  },
  "cadence_admin": false
}
```

With the YAML config above, this token grants `alice` read+write on `prod-payments` only. Any call to a different domain returns `DecisionDeny`.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Server fails to boot with `OIDC discovery: ...` | `issuerURL` wrong, Keycloak unreachable, or TLS misconfigured |
| All requests get `DecisionDeny` even with a valid-looking token | Check `aud` claim — Keycloak omits the client ID by default; add the audience mapper from step 3. Then check `realm_access.roles` actually contains the expected `cadence-*` role |
| `extracting groups claim: ... resolved to []interface {}, expected string` | `groupsAttributePath` must produce a string. Use `join(' ', @)` to flatten arrays. |
| Token rejected with `token TTL ... exceeds configured maximum` | Either lower the token lifetime in Keycloak, or raise `maxJwtTTL` in the Cadence config |
| `token has no role granting ... on domain X` | The user's token has no `cadence-{permission}` or `cadence-{permission}-{X}` role. Assign one in Keycloak and re-auth. |

## Choosing between `oauthAuthorizer` and `oidcAuthorizer`

Both can verify JWTs. Use `oidcAuthorizer` if your provider supports OIDC discovery (most do — Keycloak, Auth0, Okta, Dex, Google, etc.) — it gives you audience/issuer validation, automatic JWKS rotation, role-driven authorization, and the rollout modes documented above. Stick with `oauthAuthorizer` only if you have an existing static-JWKS / per-domain-ACL deployment that you don't want to change.

The two are mutually exclusive — enable at most one in the YAML.
