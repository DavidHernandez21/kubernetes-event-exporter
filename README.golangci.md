# Running golangci-lint in Docker

You can run [golangci-lint](https://golangci-lint.run/) in a Docker container without installing it locally. Here's how you can do it:

**None**: Currently there are several errors reported by golangci-lint. I have decided to ignore them for now.

**THIS SHOULD BE RUNNING IN CI.**

Remember to update the tag and version numbers as needed.

```bash
docker run --rm -v ${PWD}:/app -v ~/.cache/golangci-lint/v2.7.2:/root/.cache -w /app golangci/golangci-lint:v2.7.2 golangci-lint run -v
```
