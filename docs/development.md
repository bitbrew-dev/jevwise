# Development

[Documentation index](../README.md#documentation)

## Build from source

Use Go 1.27.2 or newer, matching `go.mod`. Run these commands from the checkout:

```sh
git clone https://github.com/bitbrew-dev/jevwise.git
cd jevwise
go build -o bin/jevwise ./cmd
bin/jevwise --help
bin/jevwise decide --help
```

Help does not read credentials, configuration files, or contact the API.

The current checkout uses `jevwise`; releases through v1.2.0 used `jev`. There is no `jev` alias. Existing configuration and the `jev` provider remain unchanged. Verify ownership before removing an older binary; another project may own the `jev` command.

## Version and platform builds

```sh
bin/jevwise version
bin/jevwise --version
make build-platform GOOS=windows GOARCH=amd64
make build-platform GOOS=windows GOARCH=arm64
```

- Both version forms report version, commit, and build date without credentials, config loading, or network requests.
- Plain `go build` reports `dev` with unknown metadata. Make injects metadata; only a clean exact tag is automatically a release version, otherwise `dev`.
- Release linker symbols are `github.com/bitbrew-dev/jevwise/internal/buildinfo.Version`, `.Commit`, and `.Date`.
- Make produces `build/jevwise-windows-amd64.exe` or `build/jevwise-windows-arm64.exe`. Make helpers require Unix shell tools; native Windows PowerShell can use `go build -o jevwise.exe ./cmd`, then `.\jevwise.exe --version`.

## Tests

```sh
go mod tidy -diff
go test -mod=readonly -count=1 ./...
go test -mod=readonly -race -count=1 ./...
go vet -mod=readonly ./...
go build -mod=readonly ./...
```

Tests use injected services/transports and local HTTP fixtures, not paid API requests.

- Compatibility is pinned to Python SDK v0.7.2, revision `f078f1e208a0d885154dc758344ae4fce77ac168`.
- Release/update QA uses injected HTTP responses and temporary executables, never overwriting the developer's running CLI or publishing a release.
