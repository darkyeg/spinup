# Contributing

Thanks for helping. spinup touches people's accounts and machines, so changes are held to a high bar.

- **Start with an issue** for anything bigger than a typo, so we agree on the design before code.
- **Read [AGENTS.md](AGENTS.md)**: the clean-code rules, the safety rules and where each part lives. Terms are in [GLOSSARY.md](GLOSSARY.md); how the accounts stay safe is in [docs/DESIGN.md](docs/DESIGN.md).
- **Check before you push:** `go vet ./... && go test ./...`. CI also runs them on Windows, macOS and Linux.
- **Leadership changes** (`internal/leadership`) need a case in its table test; `internal/service/machine_test.go` must keep "never two proxies at once".
- **Nothing personal** in the repo: no names, IPs, paths, emails or keys. Your own setup belongs in a private repo cloned into `local/`.
- Commit messages say what changed and why, in plain words.
