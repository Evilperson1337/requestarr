# 03: Approval policy that applies, and a count of what is waiting

**What to build:** An admin can choose media types whose requests are approved automatically, and the admin nav shows how many requests still wait for approval. Today every request waits for a manual click, the policy in the setup wizard is never applied, and nothing says anything is waiting. (UX-004; D036.)

**Blocked by:** 02 (the settings store)

**Status:** done

- [x] A setting, auto-approve by media type (movie, tv, album, song, game, audiobook), default none, readable and changeable by admins in Settings; the API and OpenAPI describe it
- [x] A request created for an auto-approved type starts `approved`, records an audit event that says it was auto-approved by policy, and notifies its subscriber as an approval does today; other types still start `requested`; existing requests are untouched; a request joined by a second person behaves as before
- [x] The admin nav shows the number of requests with status `requested` when it is above zero, and that item opens the Requests view already filtered to `requested`; requesters and viewers never see the count
- [x] The count stays correct after approving or denying (no full reload needed) and after a request is created
- [x] Tests cover: policy on and off per type; the audit event; the count for an admin and its absence for others; approval and denial still work
- [x] D036 is written in `docs/DECISIONS.md`
- [x] Integration tests run against your own throwaway database; `scripts/check.sh` passes at the end (verified by the implementer on a throwaway database, and again by the orchestrator's full `scripts/check.sh`)
