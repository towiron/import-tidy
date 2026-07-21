# Contributing

Contributions are welcome — bug reports, feature requests, and pull requests alike.

## Getting started

```bash
git clone https://github.com/towiron/import-tidy.git
cd import-tidy
make install-deps   # installs golangci-lint into ./temp/bin
```

## Workflow

1. Fork the repo and create a branch from `main` (e.g. `fix/import-order-panic`).
2. Make your change.
3. Run the checks locally:

   ```bash
   make test
   make lint
   make build
   ```

4. Open a pull request against `main`. CI (`.github/workflows/ci.yml`) runs the same `test` and `lint` checks on every PR.

## Guidelines

- Keep changes focused; avoid unrelated formatting or refactors in the same PR.
- Add or update tests in `import-tidy_test.go` for any behavior change.
- Follow the existing commit style (`feat:`, `fix:`, `refactor:`, ...).
- Code must pass `golangci-lint run -c .golangci.yaml` with no new issues.

## Reporting bugs / requesting features

Open a [GitHub issue](https://github.com/towiron/import-tidy/issues) with a clear description and, for bugs, a minimal reproduction.
