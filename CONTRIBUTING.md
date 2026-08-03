# Contributing

Keep it simple: fork the repo, make your changes, open a PR. That's it.

A few things that help but aren't hard requirements:

- Before opening the PR, run the local baseline:
  ```sh
  go build ./...
  go vet ./...
  go test ./...
  gofmt -l .
  golint ./...
  ```
- Follow the commit message style in [`docs/COMMIT_CONVENTION.md`](docs/COMMIT_CONVENTION.md)
  if you can, but don't sweat it too much.
- Describe what the PR does and why in the description — enough for a reviewer
  to understand the change without digging through the whole diff.

Questions or half-finished ideas are welcome too — open an issue or a draft PR.
