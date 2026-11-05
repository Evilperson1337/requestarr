# Installing Requestarr on Unraid

This guide takes you from nothing to a running Requestarr that you can sign in to and connect to your media library. It assumes you run Unraid and have never seen Requestarr before. Words with a special meaning here (Source, Stored secret, Master key, Private host) are explained in [`GLOSSARY.md`](../GLOSSARY.md).

Requestarr is a shared request portal: people ask for a movie, show, album, audiobook or game once, and everyone who wants it is told when it shows up in your library. It never downloads anything.

## 1. What you must bring

Requestarr is one container. It does **not** include any of these three things, on purpose, so that it does not guess at how your network is built:

1. **PostgreSQL.** All of Requestarr's data lives in one PostgreSQL database. Run a recent PostgreSQL as another container (Community Applications has several), or use the compose file in step 4, which includes one.
2. **A reverse proxy that serves Requestarr over https.** This is a small web server in front of Requestarr (Nginx Proxy Manager, SWAG, Caddy, Traefik and so on) that holds a certificate for a name such as `https://requests.example.com` and forwards to Requestarr's web port.
3. **An OIDC identity provider reachable over https.** This is the service that signs people in (Keycloak, Authentik, Authelia, Pocket ID and so on). Requestarr has no passwords of its own.

**Why https is required.** The *public URL* (the address people type) and the identity provider's address (the *issuer*) must start with `https://`. Sign-in cookies are only marked secure when the public URL is https, and the sign-in exchange carries codes and tokens that anyone on the network path could copy over plain http. So Requestarr refuses to start with an `http://` public URL or issuer rather than run unsafely.

The template and the compose file set `REQUESTARR_ENV` to `production` for you. That variable switches on these https checks, and the library address rule in step 7, and it must stay `production` on a real server (the application's own default, `development`, switches them off and is for developers only).

## 2. Create the database

You need an empty database and a user that owns it. If your PostgreSQL container is called `postgres`, open the Unraid web terminal (the `>_` icon at the top right of the Unraid page) and run:

```sh
docker exec -it postgres psql -U postgres
```

Then, at the `postgres=#` prompt (choose your own password; any characters are fine):

```sql
CREATE USER requestarr WITH PASSWORD 'choose-a-password';
CREATE DATABASE requestarr OWNER requestarr;
\q
```

Requestarr takes the database as five separate settings, so you fill in what you just created:

| Setting | Value |
| --- | --- |
| Host (`REQUESTARR_DB_HOST`) | `postgres`, the container name of your PostgreSQL container |
| Port (`REQUESTARR_DB_PORT`) | `5432` (the default; leave it unless your database uses another) |
| Name (`REQUESTARR_DB_NAME`) | `requestarr` |
| User (`REQUESTARR_DB_USER`) | `requestarr` |
| Password (`REQUESTARR_DB_PASSWORD`) | the password you chose; Requestarr escapes it itself, so any characters work |

There is also an optional `REQUESTARR_DATABASE_URL` setting that takes one complete connection URL instead; when it is set it replaces the five settings above, so most installs leave it empty.

Two details:

- A container name as the host only works when both containers are on the same **user-defined Docker network**. Unraid's default `bridge` network does *not* resolve names. Create a network if you have none (`docker network create requestarr`), then set each container's Network Type to `Custom: requestarr`. Otherwise use the server's LAN address and the PostgreSQL port as the host and port.
- The optional SSL mode (`REQUESTARR_DB_SSLMODE`, one of `disable`, `allow`, `prefer`, `require`, `verify-ca` or `verify-full`) can stay empty for a database on the same server; use `require` or stronger if the traffic crosses a network you do not control.

## 3. Create the sign-in client (Keycloak example)

In your identity provider you register Requestarr as a *client*. The steps below are for Keycloak; the generic rules follow them.

1. In the Admin Console pick (or create) the realm you want, for example `requestarr`.
2. **Clients > Create client.** Client type `OpenID Connect`, Client ID `requestarr`. Next.
3. Turn **Client authentication** on (this makes it a confidential client, one that has a secret) and keep **Standard flow** ticked. Untick Direct access grants. Next.
4. **Valid redirect URIs**: your public URL followed by `/api/v1/auth/callback`, exactly, for example `https://requests.example.com/api/v1/auth/callback`. No wildcard. Save.
5. On the **Credentials** tab copy the **Client secret**. You will paste it into Requestarr.
6. Leave the default client scopes as they are. Requestarr asks for the standard scopes `openid`, `profile` and `email` and needs nothing custom (no groups, no roles). Requestarr decides who is an admin itself (step 6), not the identity provider.
7. Create or choose the user who will be the first admin and note the user's **ID** (Users > the user > Details > ID). That value is the user's *subject*, used in step 6.

Your **issuer** for Keycloak is `https://id.example.com/realms/requestarr` (your Keycloak address, then `/realms/`, then the realm name). You can check it by opening `<issuer>/.well-known/openid-configuration` in a browser; it should show a page of JSON.

**Any other OIDC provider** needs the same four things: an https issuer URL, a client ID, a client secret (a confidential client using the authorization-code flow), and the redirect URI `<public URL>/api/v1/auth/callback` registered exactly. The provider must put a stable `sub` (subject) in the ID token, which every compliant provider does.

## 4. Install Requestarr

Pick one of two paths. Both pull the published image `ghcr.io/evilperson1337/requestarr`. While only pre-releases exist, both are pinned to an exact pre-release tag (such as `v0.1.0-rc.1`) instead of `latest`, so that you run the build that was tested.

### First, generate the two secrets

Run these in the Unraid terminal and keep each output; you will paste them in.

```sh
# Session secret: signs login sessions (random text of at least 32 bytes)
openssl rand -base64 48

# Master key: exactly 32 random bytes, as 64 hex characters or as base64
openssl rand -base64 32
```

They are different things and must be different values. The **session secret** only signs sessions; changing it just signs everyone out. The **Master key** encrypts every *Stored secret*, meaning each library or provider credential you later save in Settings, Integrations.

> **Back up the master key now, and keep it apart from the database backup.** A database dump holds the encrypted credentials; the master key opens them. If both sit in one leaked backup, the encryption achieves nothing, so store the key somewhere else (a password manager). If you lose the key, or change it, Requestarr still starts but cannot read your saved credentials and you must enter them again in Settings, Integrations.

If the master key is not valid, Requestarr stops at start and the container log says `REQUESTARR_SECRETS_KEY must be 32 bytes encoded as 64 hex characters or base64`.

### Path A: the Unraid template

1. In Unraid open **Docker**, scroll to the bottom and find **Template Repositories**. Add this address and save:

   ```
   https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/requestarr.xml
   ```

2. Click **Add Container** and choose **Requestarr** from the template list.
3. Fill in the fields:

   | Field | What to put |
   | --- | --- |
   | Network Type | `bridge` is fine if PostgreSQL is reached by LAN address. Choose your user-defined network to reach containers by name. |
   | Web Port | `8080`, unless another container uses it. Your reverse proxy points here. |
   | Database Host, Port, Name, User, Password | The values from step 2. The password field is masked. |
   | Database SSL Mode, Database URL (override) | Leave empty (advanced). |
   | Public URL | `https://requests.example.com` (https, no path). |
   | OIDC Issuer, Client ID, Client Secret | From step 3. |
   | Session Secret | The first generated value. |
   | Master Key | The second generated value. |
   | Environment | Leave `production` (advanced). |
   | Log Level | Leave `info`. |
   | Extra Private Networks | Optional; see step 7. |

   The secret fields are masked and have no default, so the template will not accept them empty. There is no appdata folder: Requestarr stores nothing on the flash drive or array itself.

4. Click **Apply**. Unraid pulls the image and starts it.

### Path B: Docker Compose (a first try, or the Compose Manager plugin)

The compose file starts Requestarr and its own PostgreSQL together, so step 2 is done for you: it creates the `requestarr` database and user with the password you put in `requestarr.env`, and passes the host, port, name, user and password to Requestarr itself. On the server:

```sh
mkdir -p /mnt/user/appdata/requestarr && cd /mnt/user/appdata/requestarr
curl -fsSLO https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/compose.yaml
curl -fsSL https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/requestarr.env.example -o requestarr.env
```

Edit `requestarr.env` and replace every `CHANGE_ME` value (the example values are deliberately rejected by the server). Then start it:

```sh
docker compose --env-file requestarr.env up -d
```

Compose refuses to start while any required value is missing. Keep `requestarr.env` private: it holds your secrets.

### Check that it is healthy

The container shows as **healthy** in the Docker tab (or `docker ps`) only when Requestarr is ready, which includes reaching its database. If it never turns healthy, read the log (Docker tab, the container's icon, Logs; or `docker logs <name>`). The log shows migrations being applied and any configuration problem by variable name, for example `REQUESTARR_PUBLIC_URL must use https outside development`.

Point your reverse proxy at `http://<server address>:8080`, then open your public URL. You should see the sign-in page.

**SWAG.** If you use the SWAG container, copy [`deploy/swag/requestarr.subdomain.conf.sample`](../deploy/swag/requestarr.subdomain.conf.sample) into SWAG's `config/nginx/proxy-confs/`, rename it to `requestarr.subdomain.conf`, and restart SWAG. It expects the Requestarr container to be named `requestarr`, to share a user-defined Docker network with SWAG, and your DNS to have a `requestarr` name. Leave SWAG's own login includes (Authelia, Authentik and so on) off: Requestarr signs people in with your OIDC provider itself. Whatever proxy you use, it must pass the `Host` and `Origin` headers through unchanged, because Requestarr checks every POST against your public URL.

## 5. Sign in, and what role new users get

Everyone who signs in for the first time is a **viewer**: they can browse and use Notify Me but cannot create requests. Nobody becomes an admin automatically, not even the first person. An admin promotes people to `requester` (can request) afterwards. Tell your users to expect this.

## 6. Become the first admin

The first admin proves they own the server with a one-time token, valid for 30 minutes, issued from inside the running container. The image has no shell, so you run the program directly through `docker exec`, in the Unraid terminal (not the container's Console button, which opens a shell that is not there):

Sign in first (you become a viewer), then open the claim page. It shows your own command with your user ID already filled in and a Copy button, so you do not look the ID up in the identity provider:

```sh
docker exec <container-name> /requestarr setup-token --subject 'YOUR-USER-ID' --stdout
```

While no administrator exists, the container's log says that the claim page shows this command.

- `<container-name>` is what `docker ps` lists: `Requestarr` for the template, something like `requestarr-requestarr-1` for compose.
- `--issuer` defaults to the Issuer setting, so you leave it out. The claim page adds it only if your sign-in issuer differs from the configured one.
- The command reads the container's own configuration, so you do not repeat any secret. It prints only the token. Copy it and clear your terminal; do not paste it into chat or tickets. The server keeps only a hash of it, and it works only for that issuer and subject.
- `--stdout` is needed because you must choose between printing the token and writing it to a file (`--token-file`), and the container has no safe place for a file.
- If it prints `setup-token requires a valid --issuer and --subject`, the subject is empty or malformed, or no issuer is configured. If the token has expired, run the command again with `--rotate`.

Then:

1. Open your public URL and sign in as the person who will be the admin.
2. On the claim page, run the command it shows, paste the token it prints into the single token field, then submit.
3. You are now the admin and land on **Get started**, a short checklist of what is left to set up. Each item opens the part of Settings that applies it (step 7).

## 7. Connect a library

Open **Settings**, then **Integrations**, and under **Media libraries** add each **Source** (Plex, Jellyfin, Navidrome, RomM or Audiobookshelf) with its address and credential. Saving an address or its credential there applies at once; no restart is needed. TMDB, IGDB and Apprise credentials, under **Metadata** and **Notifications** on the same page, apply at once too: Movies & TV and Games appear as search types as soon as their credentials are saved. Saved credentials are Stored secrets, encrypted with the master key. The **Get started** checklist (`/get-started`) shows what is still open and links here: a connected and synced library and a metadata provider are required, notifications and the default role for new people are optional, and you can dismiss it. The IGDB client ID is a field beside the IGDB secret on this page (it applies at once; `REQUESTARR_IGDB_CLIENT_ID` still wins). See [First-run setup](FIRST_RUN.md).

**Library addresses on your own network may use `http://`.** A library address must be `https://`, unless its host is a **private host**, judged only by how the address is written:

- a LAN address (`192.168.x.x`, `10.x.x.x`, `172.16.x.x` to `172.31.x.x`), a loopback or link-local address, or a `100.64.x.x` to `100.127.x.x` address (the range many VPNs use);
- a single-word name such as `plex` or `jellyfin` (a container name);
- a name ending `.local`, `.lan`, `.internal` or `.home.arpa`.

Anything else, such as `plex.example.com`, must use `https://`, so that a library token never crosses the internet in cleartext.

**The cost, stated plainly:** over `http://`, Requestarr's token or password for that library travels in cleartext between the two machines. On a home network you control that is a reasonable trade; on a network others share, use `https://`.

**Container names** (`http://jellyfin:8096`) resolve only when Requestarr and that container share a user-defined Docker network (see step 2). On the default bridge, use the server's LAN address and the container's published port.

**Unusual private networks.** To allow `http://` for a range not in the list above, set the optional **Extra Private Networks** field (the variable `REQUESTARR_HTTP_ALLOWED_NETWORKS`) to a comma-separated list of CIDR ranges and name suffixes, for example `192.0.2.0/24,.mylab`. It only adds to the built-in list, and a malformed entry stops startup and names the entry. It never relaxes the public URL, the issuer or the notification gateway address, which always need https. The exact rule is in [Integrations](INTEGRATIONS.md).

## 8. Upgrades, backups and releases

- To upgrade: back up (below), change the image tag (template: the Repository field; compose: `REQUESTARR_TAG`), and apply. Requestarr applies database migrations itself on start.
- Back up the PostgreSQL database **and**, separately, the master key and your environment settings: [Upgrade and backup](UPGRADE_AND_BACKUP.md).
- How images are built, signed and verified: [Releases](RELEASES.md). A checklist for testing a real Unraid install is in [Manual testing](MANUAL_TESTING.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Container restarts; log names a `REQUESTARR_` variable | That value is missing or invalid; the log says which. |
| Container never becomes healthy | It cannot reach PostgreSQL. Check the database host, port, user and password, and that container names resolve (step 2). |
| The provider reports an invalid redirect | The redirect URI registered there is not exactly `<public URL>/api/v1/auth/callback`. |
| A saved credential reports "stored secret unreadable" | The master key changed. Restore the original key or enter the credential again. |
| A library address is rejected | The message says whether to use https or to add the network (step 7). |
