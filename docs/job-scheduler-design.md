# agentctl Job Scheduler Design

## 目的

`agentctl` に、個別 `launchd plist` を増やさずに定期実行ジョブを管理できる job scheduler 機能を追加する。

採用方針はアプローチ D とし、以下を前提とする。

- `launchd` で常駐させる親プロセスは `agentctl-scheduler.plist` の 1 本だけにする
- 個別 job 定義は `agentctl` の SQLite DB で管理する
- 親 scheduler が定期的に DB を参照し、実行対象の job を spawn する
- モデルは Kubernetes の CronJob -> Pod に寄せる

本ドキュメントは設計のみを扱い、実装詳細やコード変更は含まない。

## 背景と課題

現状は各用途ごとに `launchd plist` を持ち、cron 的なジョブや daemon 的な処理を個別管理している。これには次の問題がある。

- ジョブ追加や schedule 変更のたびに plist 編集が必要
- 実行履歴や現在状態が plist 側に散らばり、一覧性が低い
- CLI からの追加・削除・一時停止・手動実行がしにくい
- 将来的な job 数増加で運用負荷が上がる

これを `agentctl` の DB と CLI に集約し、運用対象を「親 scheduler 1 本 + SQLite 管理 job 群」に単純化する。

## 設計原則

- `launchd` は supervisor に徹し、スケジュール判定は `agentctl` が担う
- job 定義は宣言的に DB に保存する
- 実行履歴と定義を分離し、監査可能にする
- 同一 job の多重起動は DB レベルと OS レベルの両方で抑止する
- 失敗時は黙ってリトライせず、状態を残してユーザが確認できるようにする
- 初期実装では macOS + `launchd` を前提にし、Linux systemd 等への汎化は後段で検討する

## スコープ

今回の設計対象:

- job 定義を保存する DB スキーマ
- `agentctl job ...` CLI
- 親 scheduler ループ
- 親 `launchd plist` 例
- 既存 plist からの移行方針
- 段階的な実装マイルストーン

今回の設計対象外:

- Web UI 追加
- 分散実行
- 秒単位の高精度スケジューリング
- 高度な依存関係 DAG 実行

## 全体アーキテクチャ

```text
launchd (agentctl-scheduler.plist)
  -> agentctl job scheduler run
       -> SQLite から due jobs を取得
       -> 実行ロックを確保
       -> 子プロセス起動
       -> 実行結果を DB に記録
       -> 次回実行時刻を再計算
```

責務分担は以下の通り。

- `launchd`: 親プロセスの起動、再起動、標準ログ出力先管理
- `agentctl scheduler`: poll、due 判定、spawn、排他、実行結果反映
- SQLite: job 定義、実行履歴、ロック状態、次回実行時刻

## DB スキーマ

既存 DB は `internal/store` の migration 管理を使っているため、scheduler 追加も新しい migration として入れる前提にする。

### 追加テーブル

#### `jobs`

ジョブ定義本体。

| column | type | required | description |
|---|---|---:|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | yes | job 識別子 |
| `name` | `TEXT` | yes | 人間可読な一意名 |
| `schedule` | `TEXT` | yes | cron 形式。例: `*/5 * * * *` |
| `timezone` | `TEXT` | yes | IANA TZ。初期値 `Asia/Tokyo` |
| `command` | `TEXT` | yes | 実行コマンド本体 |
| `shell` | `TEXT` | yes | 実行 shell。初期値 `/bin/zsh` |
| `working_directory` | `TEXT` | no | 実行時 cwd |
| `enabled` | `INTEGER` | yes | 1=有効, 0=無効 |
| `concurrency_policy` | `TEXT` | yes | `forbid` / `replace` / `allow` |
| `catch_up_policy` | `TEXT` | yes | `none` / `one` |
| `max_runtime_seconds` | `INTEGER` | no | 実行上限秒数 |
| `last_scheduled_at` | `TIMESTAMP` | no | 前回「実行対象と判断した」時刻 |
| `last_run_started_at` | `TIMESTAMP` | no | 前回起動時刻 |
| `last_run_finished_at` | `TIMESTAMP` | no | 前回終了時刻 |
| `last_run_status` | `TEXT` | no | `success` / `failed` / `running` / `skipped` |
| `last_exit_code` | `INTEGER` | no | 前回終了コード |
| `last_error` | `TEXT` | no | spawn 失敗や timeout 内容 |
| `next_run_at` | `TIMESTAMP` | no | scheduler が使う次回予定時刻 |
| `created_at` | `TIMESTAMP` | yes | 作成時刻 |
| `updated_at` | `TIMESTAMP` | yes | 更新時刻 |
| `deleted_at` | `TIMESTAMP` | no | 論理削除用。初期段階では未使用でも列は持たない選択可 |

制約・index:

- `UNIQUE(name)`
- `INDEX(enabled, next_run_at)`
- `INDEX(last_run_status)`

補足:

- 初期設計では `command` を 1 本の shell command として保存する
- 将来的に `args` や `env_json` を分離する余地を残す
- `status` は派生値が多いため、恒久列ではなく `enabled` + 最新 run から計算する方針を基本とする

#### `job_runs`

ジョブ実行履歴。CronJob の Job/Pod 履歴に相当。

| column | type | required | description |
|---|---|---:|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | yes | run 識別子 |
| `job_id` | `INTEGER` | yes | 親 job |
| `trigger_type` | `TEXT` | yes | `schedule` / `manual` / `recovery` |
| `scheduled_at` | `TIMESTAMP` | no | 本来実行すべきだった時刻 |
| `started_at` | `TIMESTAMP` | yes | 実際の起動時刻 |
| `finished_at` | `TIMESTAMP` | no | 終了時刻 |
| `status` | `TEXT` | yes | `running` / `success` / `failed` / `skipped` / `timeout` |
| `exit_code` | `INTEGER` | no | 終了コード |
| `pid` | `INTEGER` | no | 親が把握した PID |
| `hostname` | `TEXT` | yes | 実行ホスト識別 |
| `log_path` | `TEXT` | no | 実行ログ保存先 |
| `error_message` | `TEXT` | no | spawn 失敗や強制終了理由 |
| `created_at` | `TIMESTAMP` | yes | 作成時刻 |

制約・index:

- `FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE`
- `INDEX(job_id, started_at DESC)`
- `INDEX(status, started_at DESC)`
- `UNIQUE(job_id, scheduled_at)` を部分的に使えるなら `trigger_type='schedule'` 時だけ適用したい

補足:

- scheduled run の重複抑止には `job_id + scheduled_at` の一意性が有効
- manual run は `scheduled_at = NULL` でよい

#### `job_locks`

多重起動防止と scheduler インスタンス排他用。初期段階では 1 テーブルに集約する。

| column | type | required | description |
|---|---|---:|---|
| `lock_key` | `TEXT PRIMARY KEY` | yes | `scheduler/leader` や `job/<id>` |
| `owner` | `TEXT` | yes | `hostname:pid` |
| `expires_at` | `TIMESTAMP` | yes | lock 期限 |
| `created_at` | `TIMESTAMP` | yes | 作成時刻 |
| `updated_at` | `TIMESTAMP` | yes | 更新時刻 |

用途:

- 親 scheduler の二重起動検知
- `concurrency_policy=forbid` の job 実行抑止
- crash 時に lock を TTL で自然回収

### 追加しないもの

初期フェーズでは以下は追加しない。

- job ごとの環境変数専用テーブル
- 実行成果物アーティファクト管理
- 複数ノード前提の分散 lease

## CLI インターフェース

新しいトップレベルとして `agentctl job` を追加する。

### サブコマンド一覧

| command | purpose |
|---|---|
| `agentctl job add` | job 定義を追加 |
| `agentctl job list` | job 一覧表示 |
| `agentctl job show <name|id>` | 単一 job 詳細表示 |
| `agentctl job update <name|id>` | schedule や command を更新 |
| `agentctl job delete <name|id>` | job 削除 |
| `agentctl job enable <name|id>` | 有効化 |
| `agentctl job disable <name|id>` | 無効化 |
| `agentctl job run <name|id>` | 手動実行 |
| `agentctl job logs <name|id>` | 直近 run のログ確認 |
| `agentctl job history <name|id>` | 実行履歴表示 |
| `agentctl job scheduler run` | 親 scheduler のメインループ |
| `agentctl job scheduler once` | 1 回だけ due 判定して終了 |
| `agentctl job doctor` | schedule / DB / launchd の診断 |

ユーザ要件には `add/list/delete/run/logs` が必須だが、運用性のため `show/update/enable/disable/history/scheduler/doctor` も併記しておく。

### 代表的な CLI 例

```bash
agentctl job add manager-status \
  --schedule "*/5 * * * *" \
  --cwd "$HOME/src/github.com/chaspy/agentctl" \
  --command "agentctl manager status" \
  --concurrency forbid

agentctl job list
agentctl job run manager-status
agentctl job logs manager-status --follow
agentctl job disable manager-status
agentctl job delete manager-status
```

### `job add` の主要 flag

| flag | description |
|---|---|
| `--schedule` | cron 式。必須 |
| `--timezone` | 省略時 `Asia/Tokyo` |
| `--command` | 実行コマンド。必須 |
| `--shell` | 省略時 `/bin/zsh -lc` 相当 |
| `--cwd` | 実行ディレクトリ |
| `--concurrency` | `forbid` / `replace` / `allow` |
| `--catch-up` | `none` / `one` |
| `--max-runtime` | timeout 秒 |
| `--disabled` | 追加時に無効化 |

### 出力方針

- `list` は人間向け表形式を基本とする
- `show` / `history` / `logs` は将来の自動化を考慮し `--json` を検討する
- `run` は `run_id` を返し、`logs` と連携しやすくする

## scheduler ループ設計

### 親プロセス起動形態

親 `launchd` は常駐的に `agentctl job scheduler run` を実行する。

このプロセスは次のループを持つ。

1. scheduler leader lock を取得
2. 現在時刻以前の `enabled=1 AND next_run_at <= now()` な job を取得
3. job ごとに実行可否を判定
4. 実行する job は `job_runs` に `running` を登録して spawn
5. spawn 結果と終了結果を回収し、`jobs` と `job_runs` を更新
6. sleep して次の poll へ進む

### polling 間隔

初期値は 10 秒を推奨する。

理由:

- cron 粒度は分単位で十分
- `launchd` だけに minute 単位実行を任せるより、親常駐の方が変更反映が簡単
- SQLite poll 負荷は軽微

設定案:

- デフォルト: 10 秒
- 最小: 5 秒
- 将来は `agentctl config` か state で変更可能にする

### due 判定

各 job は `next_run_at` を持ち、scheduler は cron 式を毎回フルスキャンしない。

フロー:

1. `job add/update` 時に `next_run_at` を計算
2. run 完了時に次回 `next_run_at` を再計算
3. scheduler 復旧時、`next_run_at < now` の job を due とみなす

この方式により、一覧表示と due 検索を軽くできる。

### catch-up 方針

scheduler 停止中に missed run が発生した場合、初期方針は以下とする。

- `catch_up_policy=none`: missed run は捨て、次回時刻のみ進める
- `catch_up_policy=one`: 復旧時に 1 回だけ即時実行する

デフォルトは `none`。

理由:

- 過去分を全部 replay すると burst 実行になりやすい
- 監視系 job は「最新状態を一度見る」だけで十分なことが多い

### job 起動ロジック

初期実装は shell command を子プロセスとして直接起動する。

- 実行 API は `exec.CommandContext(shell, "-lc", command)` 相当
- `cwd` 指定があればそこへ移動
- stdout/stderr は 1 run 1 log file に保存
- `job_runs.log_path` に保存先を記録
- `max_runtime_seconds` 超過時は kill して `timeout` 扱い

ログ配置案:

```text
~/.agentctl/jobs/<job-name>/<run-id>.log
```

### 多重起動防止

`concurrency_policy` で制御する。

#### `forbid`

- 同一 job の `running` run が存在すれば新規起動しない
- `job_runs` に `skipped` を記録してもよい
- デフォルト値として推奨

#### `replace`

- 既存 run を停止して新規 run を起動
- 初期実装では複雑度が高いため、設計には含めるが milestone 1 では未実装でもよい

#### `allow`

- 並列実行を許可する
- 監視系よりバッチ系向け

### scheduler 自身の多重起動防止

親 `launchd` が再起動や手動実行で二重起動する可能性があるため、`job_locks` の `scheduler/leader` を使用する。

- lock owner は `hostname:pid`
- TTL は poll 間隔の 3 倍から 6 倍程度
- leader が heartbeat を更新
- lock が有効な間は他プロセスは standby または即終了

初期実装では「後続プロセスは即終了」が単純でよい。

### エラー処理

- DB 接続失敗: プロセスは非 0 終了し、`launchd` に再起動させる
- 個別 job spawn 失敗: scheduler 全体は継続し、その run を `failed` で記録
- ログファイル作成失敗: run を `failed` とし、標準エラーへも出す

## launchd plist 設計

親は 1 本のみ管理する。

配置先想定:

```text
~/Library/LaunchAgents/com.chaspy.agentctl-scheduler.plist
```

設定例:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.chaspy.agentctl-scheduler</string>

    <key>ProgramArguments</key>
    <array>
      <string>/usr/local/bin/agentctl</string>
      <string>job</string>
      <string>scheduler</string>
      <string>run</string>
      <string>--poll-interval</string>
      <string>10s</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>WorkingDirectory</key>
    <string>/Users/chaspy</string>

    <key>StandardOutPath</key>
    <string>/Users/chaspy/.agentctl/logs/scheduler.stdout.log</string>

    <key>StandardErrorPath</key>
    <string>/Users/chaspy/.agentctl/logs/scheduler.stderr.log</string>

    <key>EnvironmentVariables</key>
    <dict>
      <key>PATH</key>
      <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
      <key>AGENTCTL_DB_PATH</key>
      <string>/Users/chaspy/.agentctl/manager.db</string>
    </dict>
  </dict>
</plist>
```

### plist 方針

- `StartInterval` は使わず、常駐 + 内部 poll に寄せる
- `KeepAlive=true` で親プロセスを維持する
- PATH は明示する
- `agentctl` binary path は install 方法に応じて調整可能にする

## 既存 launchd plist からの移行計画

対象例:

- `manager_status_job`
- `patrol`

移行は一括置換ではなく、二重実行事故を避けるため段階的に進める。

### 移行ステップ

1. 現行 plist の一覧を棚卸しし、schedule / command / cwd / ログ出力先を表にする
2. 同等の `jobs` レコードを DB に登録する
3. `agentctl job list` と `agentctl job scheduler once` で due 判定を確認する
4. 親 `agentctl-scheduler.plist` を load する
5. 旧 plist は unload するが、ファイル自体は一定期間残す
6. `job_runs` と実ログで期待通り動いていることを確認する
7. 問題なければ旧 plist を削除する

### 移行マッピング例

#### `manager_status_job`

想定用途:

- 一定間隔で `agentctl manager status` を実行し、状態確認や記録を行う

移行案:

```bash
agentctl job add manager-status \
  --schedule "*/5 * * * *" \
  --command "agentctl manager status" \
  --cwd "/Users/chaspy/go/src/github.com/chaspy/agentctl"
```

#### `patrol`

想定用途:

- 一定間隔で監視・巡回系コマンドを走らせる

移行案:

```bash
agentctl job add patrol \
  --schedule "*/10 * * * *" \
  --command "agentctl <既存 patrol 相当コマンド>" \
  --cwd "/Users/chaspy/go/src/github.com/chaspy/agentctl"
```

### 移行時の注意点

- 旧 plist と新 scheduler の同時有効化は避ける
- まず read-only / 状態確認系 job から移行する
- 副作用の強い job は manual run と単発 schedule で先に検証する
- 既存ログ保存先がある場合、新ログパスとの対応を設計書か migration メモに残す

## 実装フェーズ分割案

### Milestone 1: 最小実用 scheduler

目的:

- DB に job を保存し、親 scheduler が due job を 1 台で実行できるようにする

範囲:

- `jobs`, `job_runs`, `job_locks` migration
- `agentctl job add/list/delete/run`
- `agentctl job scheduler once/run`
- `forbid` のみ実装
- ログファイル保存
- 親 `launchd plist` 手動設置

完了条件:

- 1 分間隔 job を追加して自動実行できる
- 手動実行で履歴とログが残る
- 二重起動しない

### Milestone 2: 運用機能の強化

範囲:

- `show/update/enable/disable/history/logs`
- timeout 管理
- `catch_up_policy`
- `doctor` コマンド
- README / docs 整備

完了条件:

- 日常運用が CLI だけで完結する
- 停止・再開・状況確認が容易になる

### Milestone 3: 既存 job の移行

範囲:

- `manager_status_job` 移行
- `patrol` 移行
- 旧 plist の段階的廃止

完了条件:

- 対象 job が親 scheduler 配下で安定稼働する
- 旧 plist を unload / 削除できる

### Milestone 4: 拡張機能

候補:

- `replace` / `allow` の厳密実装
- `--json` 出力
- Web UI 連携
- job 定義 export/import
- 将来的な Linux systemd 対応

## Open Questions

以下は設計レビューで確定したい論点。

1. `schedule` は標準 5 フィールド cron のみでよいか
2. `timezone` を job ごとに持つか、全体設定に寄せるか
3. `command` を shell string で持つか、`argv` JSON で持つか
4. `replace` を初期実装に含めるか
5. `job delete` を物理削除にするか、論理削除にするか
6. `manager_status_job` / `patrol` の実際の schedule と command を何にするか
7. ログローテーションを `agentctl` で持つか、外部運用に委ねるか

## 推奨方針

レビュー開始時点では、以下を推奨値とする。

- cron は 5 フィールドのみ
- timezone は job ごとに保持
- command は shell string で保持
- 初期 concurrency は `forbid` のみ必須
- delete は Milestone 1 では物理削除
- catch-up の default は `none`

## まとめ

本設計では `launchd` を supervisor 1 本に縮小し、job 定義と実行履歴を `agentctl` の SQLite に集約する。これにより、plist 編集中心の運用から CLI / DB 中心の運用へ移行できる。

最初は最小構成で導入し、`manager_status_job` と `patrol` を段階的に移行する。chaspy のレビュー完了後、Milestone 1 から実装に入る。
