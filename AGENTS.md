# AGENTS.md

## 言語ルール
- このリポジトリのプライマリ言語は**日本語**。会話・コードコメント・コミットメッセージ・ドキュメントはすべて日本語で記述する。
- 技術用語（API 名、型名など）は英語のままでもよい。

## プロジェクト概要
TikTok Live のエージェントアプリ（実装言語: Go）。

### 機能要件
- フォロー中のユーザのアクティブなライブを検出する
- 検出時に構成ファイルで指定されたコマンドを実行する
- コマンドはテンプレート文字列を受け入れ、アプリで定義したライブデータモデルを適用できる
  - 例: ffmpeg コマンドで rtmp/flv 配信のライブストリームを `.ts` ファイルとして保存

### 検出方法
- TikTok の webcast API（frontend 用エンドポイント）でフォロー中のユーザのアクティブなライブ一覧を取得できる
- DevTools で cURL コマンドとしてコピーし実行すると JSON が得られる（このレスポンス形式に基づいてライブデータモデルを設計する）

## 設計方針（対話で確定済み）

### 動作フロー
1. 設定した間隔でポーリングする
2. `fetch.command`（DevTools でコピーした cURL コマンド）を `sh -c` で実行し、stdout を JSON としてパースする
   - レスポンスのスキーマは「アクティブなライブあり/なし」でしか変わらないため、設定でのパス指定（output_json_path）は行わず、固定スキーマで直接パースする
3. 対象ユーザ（設定ファイルの `targets`）のライブを抽出する
4. **新規ライブIDのみ** `on_live_start.commands` を実行する
5. 検出済みライブIDはインメモリで保持する。**ポーリング結果から消えたら即削除**（プロセス再起動でリセット）

### 決定事項
- **検出方式**: 定期ポーリング
- **設定形式**: YAML
- **ポーリング間隔**: `min`/`max` を指定し、実際の間隔はその範囲から `math/rand` で毎回ランダムに決定する（API 対策の揺れ）
- **API 認証**: DevTools でコピーした cURL コマンドをそのまま実行
  - 理由: impersonate 系ライブラリはヘッダ/UA/TLS フィンガープリントの再現が不安定。cURL コマンドの直接実行は安定している
- **テンプレートエンジン**: Go 標準 `text/template`
- **コマンド実行**: goroutine で並列実行
  - `on_live_start.commands` は複数指定可能（例: ffmpeg による長時間保存 + curl による webhook 通知）
  - ライブごとに独立した goroutine で実行する
- **リトライ**: `on_live_start.commands` の各要素に `retry` を指定できる
  - `command` を指定したオブジェクト形式（旧来のプレーン文字列形式も後方互換で受け付ける）
  - ライブが**アクティブな間**は終了コードに関係なく `max_retries + 1` 回まで実行する
  - リトライ前には `monitor.Lookup` でアクティブ状態・最新データ・世代番号を確認し、ライブ終了 or 世代変化で中断する
  - 再試行時は最新のライブデータでテンプレートを再展開する
  - 間隔は `retry.interval.min`/`max` からランダムに決定する
- **シグナル転送**: SIGINT/SIGTERM 受信時、実行中のコマンドへ SIGINT を送る（`exec.Cancel` でデフォルトの SIGKILL を上書き）
  - プロセスグループ単位（`Setpgid`）で送信し、シェル→ffmpeg 等の子プロセスにも届くようにする
  - SIGINT 後 10 秒（`WaitDelay`）経過しても終了しない場合は SIGKILL で強制終了
- **ライブ終了時フック**: 実装しない（開始時のみ）
- **ログ**: `slog`（構造化ログ）

### UI（CLI / WebUI）
- **共通状態ストア**: `internal/status`。CLI と WebUI はどちらもこのストアを参照する
  - アクティブなライブ: `monitor` がポーリングごとに `SetLives` で報告（ポーリング結果から消えたら即表示から消える）
  - 起動したコマンド: `runner` が開始・終了・stdout/stderr（行数上限付きバッファ）を記録
  - ログ: `status.Handler` が `slog` をストアにも記録
- **WebUI** (`internal/webui`): `html/template` + Tailwind CSS（CDN）+ 素の JavaScript（2 秒ごとに `/api/*` を fetch して更新）
  - 起動: `-listen :8080`
  - ルート: `/`（ライブ一覧・コマンド一覧・ログ）、`/api/lives`・`/api/commands`・`/api/logs`（JSON）
- **CLI** (`internal/cli`): シンプルな画面更新式（watch 風）。端末なら画面クリアで再描画、非端末なら区切り行を出して流す
  - 起動: `-cli`
  - ライブ一覧・コマンド（stdout/stderr 末尾表示）・ログ最新 20 件を 1 秒ごとに描画
- 両 UI は同時起動可能。CLI 画面が stdout を占有するため、`-cli` 指定時は `slog` をストアのみに記録する

### 設定ファイルの構成（config.example.yaml を参照）
```yaml
polling_interval:
  min: 60s     # この範囲から毎回ランダムに決定
  max: 120s

fetch:
  command: |
    curl 'https://www.tiktok.com/webcast/following/...' -H 'user-agent: ...' -b 'sessionid=...'

targets:
  - username

on_live_start:
  commands:
    - |-
      ffmpeg -i "{{.StreamURL}}" -c copy "/tmp/{{now.Format "20060102_150405"}}-{{sanitize .Username}}-{{sanitize .Title}}.ts"
    - |-
      curl -X POST -d '{"room": "{{.RoomID}}"}' https://example.com/webhook
```

### テンプレート関数
- `on_live_start.commands` のテンプレートでは text/template に以下を追加している
  - `sanitize <string>`: `spf13/pathologize` の `Clean` で文字列をサニタイズ（ファイル名として安全な文字列にする。例: `:` `/` 除去、Windows 予約名 `CON` 等の無害化）
  - `now`: 現在時刻 `time.Time` を返す。例: `{{now.Format "20060102_150405"}}` で `yyyymmdd_hhmmss` 形式のタイムスタンプ

### 注意（YAML の制約）
- `on_live_start.commands` や `fetch.command` に `{{ }}`（テンプレート）や `{"room": ...}`（JSON ボディ）が含まれる場合、Go の YAML パーサ（yaml.v3）はプレーンスカラ内の `{` を誤解釈するため**必ずブロックスカラー（`|` または `|-`）で記述すること**。ワンライナーでも安全のため `|-` を使う。

### ライブデータモデル（webcast feed API の実レスポンスに基づく）
```go
type Live struct {
	RoomID      string   // data[].data.id_str
	Username    string   // data[].data.owner.display_id
	Nickname    string   // data[].data.owner.nickname
	Title       string   // data[].data.title（無い場合あり）
	StreamURL   string   // data[].data.stream_url.rtmp_pull_url（フォールバック: flv_pull_url.HD1）
	ViewerCount int      // data[].data.user_count
	LikeCount   int64    // data[].data.like_count
}
```
- レスポンスの `data[]` 各要素の `data` フィールドがライブルーム。`id_str` が空の要素はスキップする。
- `status_code != 0` はエラーとして扱う。

### 想定プロジェクト構成
```
tiktok-agent/
├── go.mod
├── go.sum
├── config.example.yaml  # サンプル（config.yaml は .gitignore で除外。実 cookie を誤コミットしないため）
├── .gitignore
├── cmd/tiktok-agent/main.go
└── internal/
    ├── config/          # YAML 読み込み・検証
    ├── fetcher/         # curl 実行 → JSON パース → ライブ抽出
    ├── live/            # データモデル・JSON パース
    ├── monitor/         # ポーリングループ・検出・重複管理・アクティブ状態管理（Lookup）
    ├── runner/          # テンプレート展開・コマンド実行 (goroutine)・リトライ
    ├── status/          # CLI/WebUI 共通の状態ストア（ライブ・コマンド・ログ）
    ├── webui/           # WebUI（html/template + Tailwind CDN + JS）
    └── cli/             # CLI 画面（シンプルな画面更新式）
```

## 現状
- 実装済み。ライブデータモデルは webcast_feed.json の実レスポンスに基づいて確定した。
- CLI（`-cli`）と WebUI（`-listen`）の両方に対応。共通の状態ストア `internal/status` を参照する。
- テスト: `go test ./...`（live/fetcher/monitor/runner/config/status/webui/cli すべてにテストあり）