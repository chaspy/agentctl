# herdr mux backend

agentctl の terminal multiplexer バックエンドを zellij から herdr (https://herdr.dev) に切り替えられるようにした。
切替は環境変数 `AGENTCTL_MUX` で行い、デフォルトは当面 `zellij` のまま。

## Requirements

- Worker セッションの起動・送信・画面確認・削除を herdr でも行えること
- zellij モードの挙動を変えないこと（既存セッションと `~/.agentctl/manager.db` を壊さない）
- herdr がネイティブに検知するエージェント状態 (idle/working/blocked/done) を status 判定に使うこと
- `agentctl send --verify-delivery` を herdr では画面スクレイプではなく `agent wait` で実装すること

## Design

### Backend インターフェース (`internal/mux/backend.go`)

```go
type Backend interface {
    Adapter                      // 既存の低レベル API (TypeText / SendEnter / ClearInput / DumpScreen / ListSessions / ResolveSession)
    Spawn(opts SpawnOptions) error
    Send(session, text string) error
    Read(session string) (string, error)
    Kill(session string) error
    List() ([]SessionState, error)
    Status(session string) (AgentState, error)
}
```

- `mux.ResolveBackend()` が `AGENTCTL_MUX` を見て `zellij` / `herdr` の実装を返す
- `mux.Resolve("auto")`（`agentctl send --mux auto`）も `AGENTCTL_MUX` が設定されていればそれに従う
- `cmd/mux_backend.go` に cmd 層向けの薄いヘルパー（セッション名解決、status 上書き、EXITED 掃除）をまとめた
- `SessionState` は旧 `ZellijSessionState` を拡張したもの（alias を残してある）。herdr では `CWD` / `Agent` / `State` / `Target`(pane id) が埋まる

### zellij 実装 (`internal/mux/zellij_backend.go`)

`cmd/` 各所に散在していた `env -u ZELLIJ zellij ...` の呼び出しをそのまま移植した。
`script -q /dev/null ... zellij -s <name>` でのデタッチ起動、ESC で tip を閉じる、`write-chars` + `write 13` で起動コマンド投入、`kill-session` → `delete-session` の順で削除、という挙動は従来どおり。

### herdr 実装 (`internal/mux/herdr.go`)

herdr の CLI（socket API のラッパー、JSON 出力）を `exec` で呼ぶ。生の socket は使っていない。

| agentctl の操作 | zellij | herdr |
|---|---|---|
| セッション一覧 | `zellij list-sessions` | `herdr workspace list` + `herdr pane list` |
| セッション作成 | `zellij -s <name>` (script で detach) | `herdr workspace create --cwd DIR --label <name> --no-focus` |
| エージェント起動 | `write-chars "claude ..."` + `write 13` | `herdr agent start <name> --kind claude --pane <pane> -- <args>` |
| 任意コマンド起動 (watchdog 等) | `write-chars` + `write 13` | `herdr pane run <pane> <cmd>` |
| メッセージ送信 | `write-chars` + `write 13` | `herdr agent prompt <pane> <text>`（agent 無しの pane は `pane run`） |
| 1 文字ずつ入力 / Enter / Ctrl-U | `write-chars` / `write 13` / `write 21` | `pane send-text` / `pane send-keys enter` / `pane send-keys ctrl+u` |
| 画面取得 (DumpScreen) | `dump-screen` | `herdr pane read <pane> --source visible` |
| 直近出力 (Read) | `dump-screen` | `herdr pane read <pane> --source recent-unwrapped --lines 120` |
| 削除 | `kill-session` + `delete-session` | `herdr workspace close <ws>` |
| 状態取得 | なし (`StateNone`) | `herdr agent get <pane>` の `agent_status` |
| 出力待ち | ポーリング | `herdr pane wait-output --match` (`WaitForOutput`) |
| 状態待ち | ポーリング | `herdr agent wait --until ...` (`WaitForState`) |

- **セッション名 = workspace label**。herdr は label の一意性を保証しないので、Spawn 時に同名 workspace があればエラーにする
- **メインペイン**: workspace 内で agent を持つ pane がちょうど 1 つならそれ、そうでなければ番号が最小の pane（root pane）
- **agent 名**: セッション名を `[a-z][a-z0-9_-]{0,31}` に正規化し、重複時は `-2`, `-3` を付ける。コマンドの対象には pane id を使うので名前は表示用
- **起動時の blocked**: `agent start` が `agent_not_ready`（信頼確認ダイアログなど）を返しても Spawn は成功扱いにし、従来どおり cmd 層の画面確認 (`waitForCodexReady` 等) で Enter を送る
- **blocked 中の送信**: `agent prompt` は `agent_blocked` を返して入力しない。zellij と挙動を揃えるため `pane send-text` + `send-keys enter` にフォールバックし、警告を出す
- **サーバー自動起動**: herdr の CLI はサーバーを起動しないので、`server_not_running` を受けたら `herdr server` をヘッドレスで detach 起動して 10 秒まで待つ。`AGENTCTL_HERDR_AUTOSTART=0` で無効化できる。起動時は `CLAUDECODE` / `ZELLIJ*` を環境から外す（pane 内の claude がネスト扱いにならないように）
- **名前付きセッション**: `AGENTCTL_HERDR_SESSION=<name>` で `herdr --session <name>` を全コマンドに付ける（隔離テスト用）

### status の優先順位

`agentctl list` と `agentctl state sync` は、backend の `List()` が `State` を返す場合に JSONL 推測より優先する。

| herdr `agent_status` | agentctl status |
|---|---|
| `working` | `active` |
| `blocked` | `blocked` |
| `idle` / `done` | `idle` |
| `unknown` / agent 無し | 上書きしない（JSONL 判定のまま） |

JSONL 側が `error` を検出している場合は上書きしない。

### `send --verify-delivery`

`--verify-delivery` は `--verify` の alias。

- zellij: 従来どおり 20 秒後に画面を見て、先頭 20 文字がプロンプトに残っていれば Ctrl-U → 再入力 → Enter
- herdr: `herdr agent wait --until working --until blocked --timeout 20000`。working/blocked が観測できれば送達 OK。観測できず、かつ画面にテキストが残っていれば Ctrl-U → `agent prompt` で 1 回だけ再送

さらに herdr では送信後の応答待ちも `agent wait`（settled: idle/done/blocked）で行い、その後セッションの CWD に対応する JSONL から最後の assistant メッセージを読む。これにより spawn 直後（JSONL 未作成でベースラインが取れない）でもタイムアウトしなくなった。zellij では従来の JSONL ポーリングのまま。

### DB

- `sessions.zellij_session` 列はそのまま mux セッション名（herdr では workspace label）を保持する
- `sessions.session_id` は `"<backend>-<name>"`（zellij: `zellij-…` で従来どおり、herdr: `herdr-…`）
- スキーマ変更なし

## Tasks

- [x] herdr の CLI / socket API / docs を読み、実挙動を確認
- [x] `internal/mux` に Backend インターフェースと zellij / herdr 実装
- [x] `cmd/` と `internal/web` の直接 zellij 呼び出しを backend 経由に置換（spawn / kill / resume / codex resume / manager / watchdog / monitor / state sync / web resume）
- [x] `send --verify-delivery` を herdr では `agent wait` で実装
- [x] `list` / `state sync` で herdr の state を優先
- [x] 実機検証（herdr: spawn → send → read → kill、zellij: 同じ流れでリグレッション無し）
- [x] ユニットテスト追加（agent 名正規化、メインペイン選択、status マッピング、セッション解決）

## Results

### herdr 実機ログ（`AGENTCTL_MUX=herdr`, `AGENTCTL_DB_PATH` は使い捨て DB）

```
$ AGENTCTL_MUX=herdr agentctl spawn agentctl --name test-herdr-spawn --agent claude
Using default repo profile: mode=branch(default) agent=auto(default)
Selected agent "claude" (explicit claude selection)
Created herdr workspace "test-herdr-spawn" (w4, pane w4:p1) in /Users/chaspy/go/src/github.com/chaspy/agentctl
Started claude agent "test-herdr-spawn" in herdr pane w4:p1
Spawned session "test-herdr-spawn" in /Users/chaspy/go/src/github.com/chaspy/agentctl
(12.1s)

$ herdr agent list
  "agent": "claude", "agent_status": "idle", "name": "test-herdr-spawn", "pane_id": "w4:p1"

$ AGENTCTL_MUX=herdr agentctl send test-herdr-spawn "Reply with exactly the word PING and nothing else." --verify-delivery --timeout 90
送達確認: OK
Sent instruction to herdr session "test-herdr-spawn". Waiting for response...
[claude] chaspy/agentctl (main)

PING
(16.2s)

$ AGENTCTL_MUX=herdr agentctl read agentctl
[claude] chaspy/agentctl (main) — 52s ago

PONG

$ AGENTCTL_MUX=herdr agentctl state sync
Synced 1 sessions to database
# sessions row: id=claude:chaspy/agentctl:herdr-test-herdr-spawn status=idle runtime_status=running lifecycle_state=running

$ AGENTCTL_MUX=herdr agentctl kill test-herdr-spawn
Sent /exit to session "test-herdr-spawn", waiting for Stop hook...
Killed herdr session "test-herdr-spawn"
(10.5s)

$ herdr workspace list
{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[]}}
# sessions row: status=dead runtime_status=gone lifecycle_state=stopped desired_state=stopped
```

### zellij リグレッション（`AGENTCTL_MUX` 未設定）

```
$ agentctl spawn agentctl --name test-zellij-regress --agent claude
Creating zellij session "test-zellij-regress" in /Users/chaspy/go/src/github.com/chaspy/agentctl...
Spawned session "test-zellij-regress" in /Users/chaspy/go/src/github.com/chaspy/agentctl
(7.9s)

$ agentctl send test-zellij-regress "Reply with exactly the word PONG and nothing else." --verify --timeout 60
送達確認: OK
Sent instruction to zellij session "test-zellij-regress". Waiting for response...
Error: timed out after 1m0s waiting for response   # 従来からの挙動: spawn 直後は JSONL ベースラインが無い

$ agentctl kill test-zellij-regress
Sent /exit to session "test-zellij-regress", waiting for Stop hook...
Killed zellij session "test-zellij-regress"

$ diff <(sort zellij-before.txt) <(sort zellij-after.txt) && echo SAME
SAME   # 既存の 68 個の zellij セッションは無変更
```

## Migration

1. `brew install herdr`（v0.9.5 で検証）
2. `export AGENTCTL_MUX=herdr`（Director / manager セッションの環境に入れる）
3. 最初の `agentctl spawn` で herdr サーバーが自動起動する。TUI で見るときは `herdr` を実行して attach する
4. 既存の zellij セッションはそのまま zellij で扱う。`AGENTCTL_MUX` を切り替えると `list` / `state sync` は切替先の backend だけを見るので、移行中に両方を同時に見たい場合は `AGENTCTL_MUX` を切り替えて `state sync` を 2 回実行する

## zellij との差分・注意点

- herdr には zellij の EXITED 状態が無い。`SessionState.Exited` は herdr では常に false
- `monitor` の通知先も backend 経由になった（`--target` は herdr workspace label）
- `agentctl send` の `--mux tmux` はこれまでどおり使える。herdr を使うときは `--mux herdr` か `AGENTCTL_MUX=herdr`
- herdr の `agent start` はエージェントが入力可能になるまで待つので、claude の spawn では従来の 5 秒 sleep をスキップする
- `state adopt` / `import-from-zellij` もコマンド名はそのままで backend 経由。herdr では dump-layout の代わりに pane の `cwd` / `agent` を使う
