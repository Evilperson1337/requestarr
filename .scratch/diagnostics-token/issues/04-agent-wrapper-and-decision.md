# 04: The agent wrapper, the guide and D043

Status: ready-for-agent

**What to build:** `scripts/agent/requestarr-diag.sh status|logs|runs|connections`, reading the address and token from a private directory (mode 600 enforced) and never printing the token; a guide for the operator; D043 in `docs/DECISIONS.md`. (Spec story 11; Implementation Decisions: client.)

**Blocked by:** 02

- [ ] The script refuses a token file others can read, passes the token on a private descriptor, and redacts it from any output (tested against a local fake server, as `romm-probe.sh` was)
- [ ] `logs` takes `--level` and `--after`; output is one readable line per record
- [ ] The guide covers creating the token, where to put it, revoking it and what a token can and cannot read
- [ ] D043 records the decision and the open questions' answers; `docs/OIDC_AUTH.md` and `CLAUDE.md` point to the guide
- [ ] `scripts/check.sh` passes

## Comments
