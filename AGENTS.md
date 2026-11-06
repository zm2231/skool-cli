# Agent guide

`skool-pp-cli` reads Skool communities; it has no write commands. `SKILL.md` is the user-facing
skill and `README.md` the install and usage guide.

Ask the binary for current truth instead of trusting copied command lists:

```bash
skool-pp-cli doctor --json
skool-pp-cli agent-context --pretty
skool-pp-cli which "<capability>" --json
```

Working on the code:

```bash
go build ./... && go vet ./... && go test ./...
```

Releases: push a `vX.Y.Z` tag; `.github/workflows/release.yml` runs GoReleaser, which publishes
the archives and `checksums.txt` that `install.sh` and `skool-pp-cli upgrade` read. Keep the
archive name template in `.goreleaser.yaml` and the asset name in `install.sh` in step.
