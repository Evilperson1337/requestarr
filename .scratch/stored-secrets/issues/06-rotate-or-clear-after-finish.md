# 06: Rotate or clear after Finish

**What to build:** After Finish an admin can still replace or clear any stored secret through the dedicated endpoint, while the rest of the draft stays read-only (setup_already_complete). Each change is audited with who and when, never the value.

**Blocked by:** 01

**Status:** done

- [x] Replace and clear work after Finish
- [x] Other draft writes still return setup_already_complete
- [x] Audit rows are written in the same transaction and hold no value
