# Letting an agent look at a running Requestarr: the diagnostics token

A diagnostics token (D043) lets a tool read the operational state of your instance without your session. It can call five read-only routes and nothing else. It expires, is shown once, and stops working the moment you revoke it. Use it to let an AI agent diagnose a problem (a failed sync, a sign-in that does not give the expected role) from the server's own log instead of from pasted lines.

## What a token can and cannot read

It can read, with `GET` only:

| Command | Route | What you see |
| --- | --- | --- |
| `status` | `/admin/status` | Providers and library Sources: configured or not, latest run, last success, last error |
| `logs` | `/admin/logs` | The recent server log (the same records as Settings, Logs; in memory, at most 1000) |
| `runs` | `/admin/job-runs` | The 10 newest sync runs of every Source |
| `connections` | `/admin/library-connections` | Each library address, what is missing, and where each credential comes from |
| `secrets` | `/admin/secrets` | Each stored secret's name, whether it is set, and whether the environment or a stored value supplies it |

It cannot read people (emails, roles, Users), requests, notifications, Authentication settings or the Get started checklist. It cannot change anything: every other route, and every method other than `GET`, answers 401 to a token, and a token can never create or revoke tokens. No route returns a credential value; the log never holds one (D042). The list is fixed in code and pinned by `TestTokenReadableRoutesAreExactlyThese`.

A token is valid only while the superuser who created it is still a superuser who can sign in. Demote or disable that person and their tokens stop at the next request.

## Create a token

Sign in as a superuser, open **Settings, Logs** and use the diagnostics token card.

- Give it a name that says who uses it ("claude on the laptop"; 1 to 80 plain characters).
- Choose a lifetime. The default is 30 days and the most is 90. Every token expires.
- Copy the value when it is shown. It appears once and cannot be read back; Requestarr keeps only a keyed hash. If you lose it, revoke the token and create another.
- At most 5 tokens can be active (not expired, not revoked) at once.

## Where to put it

The script reads two files from a private directory, never from an argument or the environment, so the value is not in your shell history or a process list. Run this yourself, not through the agent, in bash (type `bash` first if your login shell is fish or zsh: `read -rs -p` is bash-only):

```bash
mkdir -p ~/.config/requestarr-probe && chmod 700 ~/.config/requestarr-probe
printf '%s\n' 'https://requestarr.example' > ~/.config/requestarr-probe/diag.url
read -rs -p 'Diagnostics token: ' t && printf '%s\n' "$t" > ~/.config/requestarr-probe/diag.token; unset t
chmod 600 ~/.config/requestarr-probe/diag.token
```

`diag.url` is the bare origin you browse to (`http://` or `https://`, no path, no credentials). Set `REQUESTARR_PROBE_DIR` to use another directory. The script refuses a token file that is not mode 600 or 400, and a directory that group or others can enter.

## Use it

```bash
scripts/agent/requestarr-diag.sh status
scripts/agent/requestarr-diag.sh logs --level warn
scripts/agent/requestarr-diag.sh logs --after 120    # only records newer than seq 120
scripts/agent/requestarr-diag.sh runs
scripts/agent/requestarr-diag.sh connections
scripts/agent/requestarr-diag.sh secrets
```

`logs` prints one line per record, `seq time LEVEL message key=value ...`, oldest first. `--level` is `debug`, `info`, `warn` or `error` (default: everything kept); `--after` takes the last `seq` you saw. A restart clears the log and `seq` restarts at 1.

The script passes the token to `curl` on a private file descriptor and replaces any occurrence of it in what it prints. A refusal prints the HTTP status and Requestarr's error code (for example `HTTP 401 authentication_required`, or `429` after repeated bad attempts from your address).

Tell the agent only the command to run. Do not paste the token into a conversation, a ticket or a log.

## Revoke it

In Settings, Logs, revoke the token in its row. It is refused on its very next request. Tokens that expire need no action. Creating and revoking are recorded in the authentication audit with who did it, never the value; a failed use is logged at warning level with a fixed reason (unknown, expired, revoked, creator_not_superuser, malformed, throttled, rate_limited, not_readable, unavailable). If a token may have leaked, revoke it, then delete `diag.token`.

## Limits

A token may make 60 requests a minute. Repeated failures from one address are slowed down, on a counter of their own that sign-in does not share. Only a request with exactly one `Authorization: Bearer rqd_...` header is a token request; any other Authorization header (for example Basic auth added by a reverse proxy) is ignored and the request is judged as an ordinary session request. A malformed or unknown `rqd_` token counts as a failed attempt on any route, a valid one on a route that takes no tokens does not. Refusals that a client can repeat at will (`throttled`, `rate_limited`) are logged at most once a minute per address or token, with a count of the ones held back. Successful reads are not logged, so using a token does not fill the log it reads.
