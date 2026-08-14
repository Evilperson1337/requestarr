# Source References

The intended local source snapshots live under `references/source/checkouts/<id>/` and are ignored by Git because they are third-party repositories with independent histories/licenses and can be large.

External clone trees are deliberately excluded from repository history. Run the included fetcher on a networked development machine when local reference access is needed:

```bash
./scripts/fetch-references.sh
```

or:

```powershell
./scripts/fetch-references.ps1
```

The scripts perform shallow single-branch clones based on `manifest.yaml`. The exact revisions used for the initial research snapshot are recorded in `SNAPSHOT.md`.

## License guardrail

Reading reference code does **not** make it safe to copy. Seerr is MIT, while several *arr/game references are GPL/AGPL and Apprise is BSD. Requestarr's own license is still undecided. Prefer learning architecture/API behavior and reimplementing from documented contracts. Never ask an AI coding agent to “copy the implementation from the references.”
