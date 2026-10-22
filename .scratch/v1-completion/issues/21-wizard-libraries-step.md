# 21: Wizard Libraries step

**What to build:** An operator configures each Source, chooses its media types and validates the connection in the wizard.

**Blocked by:** 20 Wizard draft and first steps, 12 Plex music, 13 RomM games, 14 Jellyfin onto the adapter interface, 15 Navidrome

**Status:** ready-for-agent

- [ ] Each Source can be added, validated and given its media types
- [ ] A Source authoritative only for configured media types is enforced
- [ ] Invalid connections give actionable errors and do not block saving the draft
- [ ] Tests cover each Source's validation path with fakes
