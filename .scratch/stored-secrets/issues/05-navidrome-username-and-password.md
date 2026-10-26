# 05: Navidrome: plain username, stored password

**What to build:** The Navidrome username becomes an ordinary non-secret draft field, and only the password is a stored secret. The old username env reference is handled cleanly in the draft schema.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Username is saved and shown as plain draft data
- [ ] Password is stored, checked and never returned
- [ ] Existing drafts and env-configured username still load
