# Installing Requestarr on Unraid

This guide takes you from nothing to a running Requestarr that you have signed in to and connected to your identity provider and your media library. It assumes you run Unraid and have never seen Requestarr before. Words with a special meaning here (Source, Stored secret, Master key, Private host) are explained in [`GLOSSARY.md`](../GLOSSARY.md).

Requestarr is a shared request portal: people ask for a movie, show, album, audiobook or game once, and everyone who wants it is told when it shows up in your library. It never downloads anything.

## 1. What you must bring

Requestarr is one container. It does **not** include either of these two things, on purpose, so that it does not guess at how your network is built:

1. **PostgreSQL.** All of Requestarr's data lives in one PostgreSQL database. Run a recent PostgreSQL as another container (Community Applications has several), or use the compose file in step 3, which includes one.
2. **A reverse proxy that serves Requestarr over https.** This is a small web server in front of Requestarr (Nginx Proxy Manager, SWAG, Caddy, Traefik and so on) that holds a certificate for a name such as `https://requests.example.com` and forwards to Requestarr's web port. It is a prerequisite, not an extra: see the next paragraph.

You do **not** need an identity provider to install. On a fresh database Requestarr creates one built-in **local administrator** (username `admin`) and prints a one-time password in the container log. You sign in with it first, and only then connect an OIDC identity provider (Keycloak, Authentik, Authelia, Pocket ID and so on) in Settings, Authentication (step 5). Everyone else signs in through that provider; the local administrator is the only password account.

**Why https is required, and why a password makes it matter more.** The *public URL* (the address people type) must start with `https://`, and so must the identity provider's address (the *issuer*). Requestarr's sign-in cookies are marked *Secure*, which tells the browser to send them only over https, so over plain `http://` a browser would not keep your sign-in at all. And a password typed on a plain `http://` page, like the codes and tokens of an OIDC sign-in, can be read by anyone on the network path. So Requestarr refuses to start with an `http://` public URL or issuer rather than run unsafely. The local administrator password is the reason a TLS reverse proxy must be in place before you first sign in, not after.

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

## 3. Install Requestarr

Pick one of two paths. Both pull the published image `ghcr.io/evilperson1337/requestarr`. While only pre-releases exist, both are pinned to an exact pre-release tag (such as `v0.1.0-rc.5`) instead of `latest`, so that you run the build that was tested.

### First, generate the two required secrets

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
   | Initial Administrator Password | Leave empty to get a random one-time password in the container log (recommended). Or choose one of 12 to 128 characters; it is masked, used only on a fresh database, and you must change it at first sign-in. |
   | OIDC Issuer, Client ID, Client Secret (override) | Leave all three empty (advanced). You set OIDC up in the app in step 5. Fill them only if you want the environment to control OIDC; see the note after this table. |
   | Session Secret | The first generated value. |
   | Master Key | The second generated value. |
   | Environment | Leave `production` (advanced). |
   | Log Level | Leave `info`. |
   | Extra Private Networks | Optional; see step 7. |

   If you do set any one of the three OIDC override fields, all three come from the environment and they override anything saved in Settings, Authentication (step 6 explains what you then see). The required secret fields are masked and have no default, so the template will not accept them empty. There is no appdata folder: Requestarr stores nothing on the flash drive or array itself.

4. Click **Apply**. Unraid pulls the image and starts it.

### Path B: Docker Compose (a first try, or the Compose Manager plugin)

The compose file starts Requestarr and its own PostgreSQL together, so step 2 is done for you: it creates the `requestarr` database and user with the password you put in `requestarr.env`, and passes the host, port, name, user and password to Requestarr itself. On the server:

```sh
mkdir -p /mnt/user/appdata/requestarr && cd /mnt/user/appdata/requestarr
curl -fsSLO https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/compose.yaml
curl -fsSL https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/requestarr.env.example -o requestarr.env
```

Edit `requestarr.env` and replace every `CHANGE_ME` value (the example values are deliberately rejected by the server). The OIDC lines and `REQUESTARR_ADMIN_PASSWORD` in that file are optional and commented out; leave them so. Then start it:

```sh
docker compose --env-file requestarr.env up -d
```

Compose refuses to start while any required value is missing. Keep `requestarr.env` private: it holds your secrets.

### Check that it is healthy

The container shows as **healthy** in the Docker tab (or `docker ps`) only when Requestarr is ready, which includes reaching its database. If it never turns healthy, read the log (Docker tab, the container's icon, Logs; or `docker logs <name>`). The log shows migrations being applied and any configuration problem by variable name, for example `REQUESTARR_PUBLIC_URL must use https outside development`.

Point your reverse proxy at `http://<server address>:8080`, then open your public URL. You should see the sign-in page (with no OIDC set up yet it is the administrator form).

**SWAG.** If you use the SWAG container, copy [`deploy/swag/requestarr.subdomain.conf.sample`](../deploy/swag/requestarr.subdomain.conf.sample) into SWAG's `config/nginx/proxy-confs/`, rename it to `requestarr.subdomain.conf`, and restart SWAG. It expects the Requestarr container to be named `requestarr`, to share a user-defined Docker network with SWAG, and your DNS to have a `requestarr` name. Leave SWAG's own login includes (Authelia, Authentik and so on) off: Requestarr signs people in itself (the local administrator now, your OIDC provider once you set it up). Whatever proxy you use, it must pass the `Host` and `Origin` headers through unchanged, because Requestarr checks every POST against your public URL.

**Telling visitors apart (optional, advanced).** After failed sign-ins Requestarr slows its answers down per account name and per client address. Behind a reverse proxy every visitor reaches Requestarr from the proxy's address, so out of the box they all share one address and one noisy visitor makes everyone's failed attempts slower (a correct password is never delayed or refused for that). To let Requestarr see the real visitor, set `REQUESTARR_TRUSTED_PROXIES` (template field "Trusted Proxies", under Show more settings) to your proxy's address or network, for example `172.18.0.0/16` (comma-separated for several). Only a connection from one of those addresses is believed when it sends `X-Forwarded-For`, and Requestarr then takes the rightmost address in that header that is not itself one of your proxies. Leave it empty if you are unsure: the header is then ignored, which is safe but less precise. Never list a network that other machines can reach directly, because anyone inside it could forge the header. An invalid value stops the container at start with an error naming the variable.

## 4. Sign in for the first time

On a fresh database, Requestarr creates the local administrator when it starts and writes the one-time password to the container log, once.

1. In Unraid open the **Docker** tab, click the Requestarr container's icon and choose **Logs** (or run `docker logs <name>`).
2. Find the line that starts `Requestarr created the local administrator. Sign in as 'admin' at https://requests.example.com/login with the one-time password in this line`. The password is the value of `one_time_password=` at the end of that line. Anyone who can read the log can read it, so do not paste the line into a chat or a ticket.
   - If you set the Initial Administrator Password (`REQUESTARR_ADMIN_PASSWORD`) instead, the line says the account was created with the password from that variable and shows no password; use the value you set.
   - The line is written only when the account is created, which is the first start on an empty database. If you missed it, use the recovery command in step 6.
3. Open your public address at `/login`, for example `https://requests.example.com/login`. Sign in as `admin` with that password.
4. Requestarr asks you to **Choose a new password** (12 to 128 characters, no other rules) before it lets you do anything else. Choose one and keep it in a password manager.

You are now signed in as an administrator. The **Get started** page lists what is still open and links to the Settings that fix it; see [First-run setup](FIRST_RUN.md).

## 5. Set up OIDC for everyone else (Keycloak example)

OIDC is configured in the app, not in the container settings. In your identity provider you register Requestarr as a *client*; then you enter its details in **Settings, Authentication**. The steps below are for Keycloak; the generic rules follow them.

1. In the Keycloak Admin Console pick (or create) the realm you want, for example `requestarr`.
2. **Clients > Create client.** Client type `OpenID Connect`, Client ID `requestarr`. Next.
3. Turn **Client authentication** on (this makes it a confidential client, one that has a secret) and keep **Standard flow** ticked. Untick Direct access grants. Next.
4. **Valid redirect URIs**: the exact **Redirect address** shown in Settings, Authentication, which is your public URL followed by `/api/v1/auth/callback`, for example `https://requests.example.com/api/v1/auth/callback`. The **Copy redirect address** button there copies it. No wildcard. Save.
5. On the **Credentials** tab copy the **Client secret**.
6. Leave the default client scopes as they are. Requestarr requests exactly the standard scopes `openid`, `profile` and `email` and needs nothing custom (no groups, no roles). Requestarr decides who is an administrator itself (below), not the identity provider.

In Requestarr open **Settings**, then the **Authentication** section, and in the **OpenID Connect** card fill in:

| Field | Value |
| --- | --- |
| Provider name | Shown on the sign-in button, for example `Keycloak` |
| Client ID | `requestarr` |
| Issuer | `https://id.example.com/realms/requestarr` (your Keycloak address, then `/realms/`, then the realm name) |
| Client secret | The secret from the Credentials tab. It is stored encrypted with the master key and never shown again; leave it blank later to keep the saved one. |

Then:

1. Press **Save OIDC settings**.
2. Press **Test OIDC connection**. It contacts the issuer and checks its published settings. It does **not** prove that the client secret is right or that a person can sign in; the next steps prove that.
3. Switch on **Enable OIDC**. The status then reads `On · <provider name>`. Changes apply at once; there is no restart.
4. Open a private browser window and your public address. It now offers **Sign in with** your provider name. Sign in once with the account you want to make an administrator. The first time, that person is a **viewer**.

You can check the issuer in a browser by opening `<issuer>/.well-known/openid-configuration`; it should show a page of JSON.

**Any other OIDC provider** needs the same things: an https issuer URL, a client ID, a client secret (a confidential client using the authorization-code flow), and the redirect address from Settings, Authentication registered exactly. The provider must put a stable `sub` (subject) in the ID token, which every compliant provider does.

If OIDC breaks later (the provider is down, a secret expired), the administrator form at `/login` keeps working, so OIDC cannot lock you out. The OIDC sign-in page also carries an **Administrator sign-in** link.

### Promote the first OIDC person, and the role new people get

No OIDC sign-in becomes an administrator automatically. Back in the administrator session, open **Settings, Users**, find the person in **User management** and change their role to `admin` (or `requester`, who can create requests). The local administrator is shown with a **Local administrator** badge and "Always administrator"; its role cannot be changed. You do not need the person's identity-provider user ID.

Everyone else who signs in for the first time is a **viewer**: they can browse and use Notify Me but cannot create requests. In the same tab, **New people** has the setting *Role for people who sign in for the first time*, either `viewer` (the default) or `requester`; it never applies to people who already have an account and cannot be administrator. Every administrator is notified when someone signs in for the first time.

## 6. Recover access, and the other ways OIDC can be set

**Forgotten local administrator password.** Use the Unraid terminal (or any shell on the server), not the container's Console button (the image has no shell):

```sh
docker exec <container-name> /requestarr reset-admin-password
```

If `docker exec` fails with `OCI runtime exec failed: open /run/user/0/runc-process…: no such file or directory`, the problem is Unraid's Docker, not Requestarr: the folder it wants for the exec process does not exist (it lives in memory and is gone after a reboot). Create it and run the command again:

```sh
mkdir -p /run/user/0
```

If exec still will not work, run the same command in a one-off container on the same network, with the database settings of your Requestarr container (the template's database fields: `REQUESTARR_DB_HOST`, `REQUESTARR_DB_PORT`, `REQUESTARR_DB_NAME`, `REQUESTARR_DB_USER`, `REQUESTARR_DB_PASSWORD`), and substitute the image tag you run:

```sh
docker run --rm --network <your-network> \
  -e REQUESTARR_DB_HOST=<db-host> -e REQUESTARR_DB_NAME=requestarr \
  -e REQUESTARR_DB_USER=<db-user> -e REQUESTARR_DB_PASSWORD=<db-password> \
  ghcr.io/evilperson1337/requestarr:<tag> reset-admin-password
```

`<container-name>` is what `docker ps` lists (`Requestarr` for the template). The command uses the container's own database settings, prints one new random password and nothing else, ends all of the local administrator's sessions, turns the account back on if an administrator had disabled it, and forces a change at the next sign-in. Keep the printed password private and do not paste it into a support ticket. If the account does not exist (an upgraded instance with no administrator and no local administrator), the command creates it, prints the same single password and says on the error output that it was created; it does not re-enable a disabled account.

**OIDC set in the environment.** If you set the OIDC override fields (`REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID`, `REQUESTARR_OIDC_CLIENT_SECRET`), Settings, Authentication shows the card as **Set by environment** and read-only. If any one of the three is set, all three are taken from the environment and anything saved in the app is ignored; the card then says **Saved settings are ignored**, lists which saved values differ, and offers **Discard saved settings**. If only some of the three are set, the card says the environment configuration is incomplete and names the missing variables; OIDC stays off until all three are set. To manage OIDC in the app again, remove the three variables and restart the container.

**Upgrading an existing instance.** An instance that already has users gets no local administrator automatically, and an OIDC configuration in its environment keeps working unchanged. An existing administrator can create the local administrator in **Settings, Authentication** with **Create local administrator**: the password is shown once on screen and must be changed at first sign-in. The same card can disable and re-enable the account and change its password.

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
| The provider reports an invalid redirect | The redirect URI registered there is not exactly the Redirect address shown in Settings, Authentication (`<public URL>/api/v1/auth/callback`). |
| I cannot find the one-time password | It is logged once, when the account is created on a fresh database. A database that already has people (an upgrade, or an earlier test run) gets no local administrator, and the log says so in one line. Run `reset-admin-password` (step 6) to create it and print a one-time password. |
| Signing in at `/login` does not stick | The public address is not https, so the browser drops the Secure cookie. |
| A saved credential reports "stored secret unreadable" | The master key changed. Restore the original key or enter the credential again. |
| A library address is rejected | The message says whether to use https or to add the network (step 7). |

**The log says `database is unreachable: ...`.** The words after the colon are the cause. The log shows the address and database name Requestarr is using but never the user name or password. At start Requestarr tries **three times, five seconds apart**, because the database container may still be coming up, and logs `database not ready, will try again` after each failed try. A wrong password or a missing database is not retried, since waiting cannot fix it; those stop at once. If all three fail, the last line ends `(gave up after 3 attempts, 5s apart)`.

| Log text | What it means, and what to do |
| --- | --- |
| `the host name "..." could not be resolved` | The name is not known from inside this container. On Unraid's default bridge network a container name does not resolve: put Requestarr and PostgreSQL on the same user-defined network (the template's Network Type), or use the PostgreSQL container's address. |
| `timed out connecting to host:port` | Nothing answered within 5 seconds. Check that the host name resolves and that the address is reachable from this container; a wrong address or a firewall looks like this. |
| `nothing accepted the connection at host:port` | The host answered but nothing listens on that port: PostgreSQL is not running (Requestarr already waited about 10 seconds for it; start Requestarr again once it is up), or the port is wrong. |
| `PostgreSQL rejected the user name or password` | Check the user and password fields. Any characters are fine in the password. |
| `the database "..." does not exist on the server` | Create it (step 2) or correct the name. |
| `PostgreSQL is still starting up` | It was still starting for all three tries. Wait a moment and start Requestarr again. |
| `a secure (TLS) connection ... could not be negotiated` | Set the SSL mode field to match the server (`disable` for a server without TLS). |
