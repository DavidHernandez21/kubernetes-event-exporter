# Running golangci-lint locally in Docker

You can run [golangci-lint](https://golangci-lint.run/) in a Docker container without installing it locally. Here's how you can do it:

Use the latest release tag from https://github.com/golangci/golangci-lint/releases/latest.

```bash
GOLANGCI_VERSION=v2.11.1
docker run --rm -v ${PWD}:/app -v ~/.cache/golangci-lint/${GOLANGCI_VERSION}:/root/.cache -w /app golangci/golangci-lint:${GOLANGCI_VERSION} golangci-lint run -v
```
