# 04: The agent wrapper, the guide and D043

Status: needs-review

**What to build:** `scripts/agent/requestarr-diag.sh status|logs|runs|connections`, reading the address and token from a private directory (mode 600 enforced) and never printing the token; a guide for the operator; D043 in `docs/DECISIONS.md`. (Spec story 11; Implementation Decisions: client.)

**Blocked by:** 02

- [x] The script refuses a token file others can read, passes the token on a private descriptor, and redacts it from any output (tested against a local fake server, as `romm-probe.sh` was)
- [x] `logs` takes `--level` and `--after`; output is one readable line per record
- [ ] The guide covers creating the token, where to put it, revoking it and what a token can and cannot read
- [x] D043 records the decision and the open questions' answers; `docs/OIDC_AUTH.md` and `CLAUDE.md` point to the guide
- [x] `scripts/check.sh` passes

## Comments

How each criterion was checked (`scripts/agent/requestarr-diag-test.sh` runs the script against a Python fake server that accepts one canary token and echoes it in a log message, an attribute, a run error and an error body; wired into `scripts/check.sh` after the export test):
- Refusal and redaction: mode 644 and 640 token files, a 755 directory, an address with credentials or a path, and a missing token file all exit non-zero before any request (the server's request log did not grow). A curl shim on PATH records every argument curl receives; the canary is not in it (the token goes through `curl -K <(...)`). Every subcommand prints the echoed canary as `[token]`, and the output for a rejected token holds no canary.
- Logs: `--level warn` and `--level error --after 2` return the expected record counts and the server's request log shows `level=error` and `after=2` were sent; an invalid level, a non-numeric `--after` and an unknown subcommand are refused. Output is one line per record (a newline inside a message is folded): `seq time LEVEL message key=value`.
- Guide (left unticked): `docs/DIAGNOSTICS_TOKEN.md` covers creating, where to put the token, revoking, and the readable and unreadable lists, checked by reading against the spec and ticket 02's notes. I could not check its wording about the Settings, Logs card (name, lifetime choice, revoke in a row) because ticket 03 is built in parallel; confirm it once 03 lands, then tick.
- D043 (the last number before it was D042) records the decision and the three answers; `docs/OIDC_AUTH.md` and `CLAUDE.md` point to the guide.
- `scripts/check.sh` exit 0 (PostgreSQL integration and Playwright included, 365 browser tests passed).
- Also not checked: a run against a real Requestarr with a real token. The fake server follows `api/openapi.yaml`, not the running server.
