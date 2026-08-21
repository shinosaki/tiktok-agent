# tiktok-agent

## Build

```bash
task build

# $ task --list
# task: Available tasks for this project:
# * build:               linux/amd64 向けビルド（static + dynamic の両方を生成）
# * clean:               ビルド成果物を削除
# * default:             デフォルト（static + dynamic ビルド）
# * test:                テストを実行
# * vet:                 go vet を実行
# * build:dynamic:       動的リンクビルド（CGO 有効）
# * build:static:        静的リンクビルド（CGO 無効）
```

## License
[MIT](./LICENSE)

## Author
[shinosaki](https://shinosaki.com)

Written using OpenCode
