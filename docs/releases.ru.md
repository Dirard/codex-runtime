# Releases

Этот документ для maintainers.

Release публикует только `codex-runtime-gateway` binary. Codex itself не
пакуется внутрь release.

## Что Публикуется

GoReleaser собирает gateway для:

- Linux `amd64` и `arm64`;
- macOS `amd64` и `arm64`;
- Windows `amd64` и `arm64`.

Linux/macOS artifacts - `tar.gz`. Windows artifacts - `zip`. Также публикуется
SHA-256 checksum file.

## Локальная Проверка

```sh
goreleaser check
goreleaser build --snapshot --clean
```

Если GoReleaser не установлен:

```sh
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest build --snapshot --clean
```

Полезные проверки перед release:

```sh
go test -count=1 ./...
go vet ./...
git diff --check
```

## Version

Binary поддерживает:

```sh
codex-runtime-gateway --version
```

## Публикация

Release запускается tag-ом вида `v*`:

```sh
git tag v0.0.2
git push origin v0.0.2
```

Перед tag убедитесь, что README/docs не обещают bundled Codex. Пользователь
release все равно настраивает `codex_binary`, `codex_home`,
`workflow_storage_dir` и gateway-token source сам.
