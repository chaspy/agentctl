# agentctl Job Scheduler Design

## 目的

`agentctl` に job scheduler 機能を追加し、個別 `launchd plist` の増殖を止める。

採用方針はアプローチ D とし、以下を前提とする。

- 親 `launchd` は `agentctl-scheduler.plist` の 1 本だけを持つ
- 個別 job 定義は `agentctl` の SQLite DB で管理する
- 親 scheduler が定期的に DB を参照し、due な job を `agentctl` の内部機能として実行する
- モデルは Kubernetes の CronJob -> Job に寄せる

## 問題意識

初期案では job 定義に `command` 文字列を持たせ、scheduler が shell command を実行する構造を想定していた。しかしこの形だと実質的に cron の再実装になり、`agentctl` 固有の価値が薄い。

`agentctl` にとって job の実態は自由な shell command ではなく、主に次の 2 種類である。

- `agentctl spawn`
- `agentctl send`

このため job 定義は command 文字列ではなく、`agentctl` が直接解釈できる構造化フィールドとして持つ。

## 設計原則

- scheduler は shell 実行基盤ではなく、`agentctl` オーケストレータとして振る舞う
- job 定義は `spawn` / `send` の意図を構造化して保存する
- 指示文は DB に可読なデータとして残す
- shell quoting や escaping に依存しない
- 実行履歴と job 定義を分離して監査可能にする
- 同一 job の多重起動は DB レベルで抑止する
- 初期実装は macOS + `launchd` 前提に絞る

## スコープ

今回の設計対象:

- 構造化 job 定義の DB スキーマ
- `agentctl job ...` CLI
- 親 scheduler ループ
- 親 `launchd plist` 例
- 既存 plist からの移行方針
- 段階的な実装マイルストーン

今回の設計対象外:

- 任意 shell command 実行
- Web UI
- 分散実行
- 秒単位の高精度スケジューリング

## 全体アーキテクチャ

```text
launchd (agentctl-scheduler.plist)
  -> agentctl job scheduler run
       -> SQLite から due jobs を取得
       -> 実行ロックを確保
       -> job.action を解釈
       -> spawn / send / state-sync / command を実行
       -> 実行結果を DB に記録
       -> 次回実行時刻を再計算
```

責務分担:

- `launchd`: 親 scheduler の起動と再起動
- `agentctl scheduler`: due 判定、排他、action 実行、結果保存
- SQLite: job 定義、run 履歴、lock 状態、次回実行時刻

## Job モデル

job は `action` によって種類が分かれる。

### `action=spawn`

新しい agent session を起動し、初期 instruction を投入する。

対応イメージ:

```bash
agentctl spawn <repo> --branch <branch> --agent <agent> --message <instruction>
```

主用途:

- 定期レポート作成
- 差分レビュー
- 朝会前の集計や比較
- PR 作成を含む定期タスク

### `action=send`

既存 session に対して instruction を送る。

対応イメージ:

```bash
agentctl send <session> <instruction>
```

主用途:

- 常駐 patrol session への巡回依頼
- manager session への定期確認依頼
- 既存 loop session へのトリガー

### `action=state-sync`

`agentctl state sync` 相当を shell 経由ではなく内部 API として実行する。

対応イメージ:

```bash
agentctl state sync --agent <all|claude|codex>
```

主用途:

- session / PR metadata の定期同期
- `Decision -> Attempt -> Outcome` の自動確定
- 既存 `Outcome` の PR lifecycle metadata refresh

### `action=command`

既存の deterministic script を移行するための互換 action。
新規の agentctl 内部処理は、可能な限り専用 action に切り出す。

## DB スキーマ

既存 DB は `internal/store` の migration 管理を使っているため、scheduler 追加も新しい migration として入れる。

### `jobs`

job 定義本体。

| column | type | required | description |
|---|---|---:|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | yes | job 識別子 |
| `name` | `TEXT` | yes | 人間可読な一意名 |
| `schedule` | `TEXT` | yes | cron 形式。例: `0 9 * * 1` |
| `timezone` | `TEXT` | yes | IANA TZ。初期値 `Asia/Tokyo` |
| `action` | `TEXT` | yes | `spawn`, `send`, `command`, or `state-sync` |
| `repo` | `TEXT` | no | `spawn` の対象 repo。例: `chaspy/myassistant` |
| `session` | `TEXT` | no | `send` の対象 session 名 |
| `branch` | `TEXT` | no | `spawn` の branch |
| `agent` | `TEXT` | no | `spawn` 時の agent、または `state-sync` の filter。`all` / `claude` / `codex` / `auto` |
| `instruction` | `TEXT` | action dependent | LLM に渡す指示文、または command 文字列 |
| `enabled` | `INTEGER` | yes | 1=有効, 0=無効 |
| `concurrency_policy` | `TEXT` | yes | `forbid` / `replace` / `allow` |
| `catch_up_policy` | `TEXT` | yes | `none` / `one` |
| `last_scheduled_at` | `TIMESTAMP` | no | 前回 due と判断した時刻 |
| `last_run_started_at` | `TIMESTAMP` | no | 前回起動時刻 |
| `last_run_finished_at` | `TIMESTAMP` | no | 前回終了時刻 |
| `last_run_status` | `TEXT` | no | `success` / `failed` / `running` / `skipped` |
| `last_error` | `TEXT` | no | 実行失敗内容 |
| `next_run_at` | `TIMESTAMP` | no | scheduler が参照する次回予定時刻 |
| `created_at` | `TIMESTAMP` | yes | 作成時刻 |
| `updated_at` | `TIMESTAMP` | yes | 更新時刻 |

制約・index:

- `UNIQUE(name)`
- `CHECK(action IN ('spawn', 'send', 'command', 'state-sync'))`
- `CHECK(concurrency_policy IN ('forbid', 'replace', 'allow'))`
- `CHECK(catch_up_policy IN ('none', 'one'))`
- `INDEX(enabled, next_run_at)`
- `INDEX(action, enabled)`

action ごとの必須条件:

- `spawn`: `repo`, `instruction` 必須
- `send`: `session`, `instruction` 必須

設計メモ:

- `spawn` では `repo` / `branch` / `agent` / `instruction` をそのまま `runSpawn` 相当へ渡す
- `send` では `session` / `instruction` を `runSend` 相当へ渡す
- `instruction` が job 定義の中心データになる
- shell command は持たない

### `job_runs`

job 実行履歴。

| column | type | required | description |
|---|---|---:|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | yes | run 識別子 |
| `job_id` | `INTEGER` | yes | 親 job |
| `trigger_type` | `TEXT` | yes | `schedule` / `manual` / `recovery` |
| `scheduled_at` | `TIMESTAMP` | no | 本来の実行時刻 |
| `started_at` | `TIMESTAMP` | yes | 実際の開始時刻 |
| `finished_at` | `TIMESTAMP` | no | 終了時刻 |
| `status` | `TEXT` | yes | `running` / `success` / `failed` / `skipped` |
| `action` | `TEXT` | yes | 実行した action のスナップショット |
| `repo` | `TEXT` | no | 実行時の repo |
| `session` | `TEXT` | no | 実行時の session |
| `branch` | `TEXT` | no | 実行時の branch |
| `agent` | `TEXT` | no | 実行時の agent |
| `instruction` | `TEXT` | yes | 実行時の instruction スナップショット |
| `result_session` | `TEXT` | no | `spawn` 結果の session 名 |
| `result_session_id` | `TEXT` | no | DB 上の session ID |
| `error_message` | `TEXT` | no | 実行失敗内容 |
| `created_at` | `TIMESTAMP` | yes | 作成時刻 |

制約・index:

- `FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE`
- `INDEX(job_id, started_at DESC)`
- `INDEX(status, started_at DESC)`
- `INDEX(action, started_at DESC)`

設計メモ:

- `job_runs` には job 定義の重要フィールドをスナップショットとして残す
- 後から job 定義が変わっても、当時どの instruction を送ったか追跡できる
- `spawn` の場合は生成した session 名や session ID を残す

### `job_locks`

多重起動防止と scheduler 排他用。

| column | type | required | description |
|---|---|---:|---|
| `lock_key` | `TEXT PRIMARY KEY` | yes | `scheduler/leader` や `job/<id>` |
| `owner` | `TEXT` | yes | `hostname:pid` |
| `expires_at` | `TIMESTAMP` | yes | lock 期限 |
| `created_at` | `TIMESTAMP` | yes | 作成時刻 |
| `updated_at` | `TIMESTAMP` | yes | 更新時刻 |

用途:

- 親 scheduler の二重起動防止
- `concurrency_policy=forbid` の job 実行抑止
- crash 時の TTL 回収

## CLI インターフェース

トップレベルに `agentctl job` を追加する。

### サブコマンド一覧

| command | purpose |
|---|---|
| `agentctl job add` | job 定義を追加 |
| `agentctl job list` | job 一覧表示 |
| `agentctl job show <name|id>` | 単一 job 詳細表示 |
| `agentctl job update <name|id>` | job 定義更新 |
| `agentctl job delete <name|id>` | job 削除 |
| `agentctl job enable <name|id>` | 有効化 |
| `agentctl job disable <name|id>` | 無効化 |
| `agentctl job run <name|id>` | 手動実行 |
| `agentctl job logs <name|id>` | 実行履歴確認 |
| `agentctl job history <name|id>` | 実行履歴一覧 |
| `agentctl job scheduler run` | 親 scheduler ループ |
| `agentctl job scheduler once` | due 判定を 1 回だけ実施 |

### `job add` の主要 flag

共通:

| flag | description |
|---|---|
| `--schedule` | cron 式。必須 |
| `--timezone` | 省略時 `Asia/Tokyo` |
| `--action` | `spawn`, `send`, `command`, or `state-sync`。必須 |
| `--instruction` | `spawn` / `send` / `command` では必須。`state-sync` では不要 |
| `--concurrency` | `forbid` / `replace` / `allow` |
| `--catch-up` | `none` / `one` |
| `--disabled` | 追加時に無効化 |

`action=spawn`:

| flag | description |
|---|---|
| `--repo` | 対象リポジトリ。必須 |
| `--branch` | ブランチ名 |
| `--agent` | `claude` / `codex` / `auto` |

`action=send`:

| flag | description |
|---|---|
| `--session` | 対象 session 名。必須 |

`action=state-sync`:

| flag | description |
|---|---|
| `--agent` | 同期対象 agent filter。省略時 `all` |

### CLI 例

```bash
agentctl job add weekly-report \
  --schedule '0 9 * * 1' \
  --action spawn \
  --repo chaspy/myassistant \
  --branch feat/weekly-report \
  --agent codex \
  --instruction '比較レポートを更新してPRを出してください'

agentctl job add daily-patrol \
  --schedule '0 8 * * *' \
  --action send \
  --session patrol-codex \
  --instruction '今日の巡回をしてください'

agentctl job add agentctl-state-sync \
  --schedule '*/5 * * * *' \
  --action state-sync \
  --agent all
```

### 出力方針

- `list` では `schedule`, `action`, `target`, `next_run_at`, `last_run_status` を見せる
- `show` では instruction を含む構造化定義を表示する
- `history` では実行時の instruction スナップショットを辿れるようにする
- `logs` は shell log ではなく run 履歴と result 情報の確認に寄せる

## scheduler ループ設計

### 親プロセス起動形態

親 `launchd` は `agentctl job scheduler run` を常駐実行する。

ループ:

1. leader lock を取得
2. `enabled=1 AND next_run_at <= now()` の job を取得
3. action ごとに実行可否を判定
4. `job_runs` に `running` を登録
5. `spawn` または `send` を内部 API として実行
6. 実行結果を `jobs` と `job_runs` に反映
7. `next_run_at` を再計算して sleep

### polling 間隔

初期値は 10 秒を推奨する。

理由:

- cron 粒度は分単位で十分
- due 判定は軽い
- DB 反映や job 追加直後の追従が速い

### due 判定

各 job は `next_run_at` を持つ。

フロー:

1. `job add/update` 時に `next_run_at` を計算
2. run 完了時に次回 `next_run_at` を再計算
3. scheduler 復旧時、`next_run_at < now` の job を due とみなす

### catch-up 方針

- `none`: missed run は捨てる
- `one`: 復旧時に 1 回だけ即時実行する

デフォルトは `none`。

### action 実行ロジック

#### `spawn`

scheduler は shell command を組み立てず、`runSpawn` 相当の内部処理を直接呼ぶ。

入力:

- `repo`
- `branch`
- `agent`
- `instruction`

結果として記録したいもの:

- 生成された zellij session 名
- DB に登録された session ID
- 実行エラー

#### `send`

scheduler は `runSend` 相当の内部処理を直接呼ぶ。

入力:

- `session`
- `instruction`

結果として記録したいもの:

- 対象 session 名
- 配信成功 / 失敗
- wait の成否

### 多重起動防止

`concurrency_policy` で制御する。

#### `forbid`

- 同一 job の `running` run があれば新規実行しない
- 初期 default

#### `replace`

- 既存 run を中断して新規 run を優先
- 初期実装では保留でもよい

#### `allow`

- 並列実行を許可

### scheduler 自身の多重起動防止

`job_locks` の `scheduler/leader` を使う。

- owner は `hostname:pid`
- TTL は poll 間隔の数倍
- 初期実装では後続プロセスは即終了でよい

### エラー処理

- DB 接続失敗: scheduler 全体を非 0 終了
- `spawn` / `send` 実行失敗: 該当 run を `failed` で記録して継続
- 入力不正な job: `failed` として記録し、明示的に可視化する

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

方針:

- `StartInterval` は使わず常駐 + 内部 poll に寄せる
- `KeepAlive=true` で親プロセスを維持する
- scheduler が内部的に `spawn` / `send` を実行する

## 既存 launchd plist からの移行計画

対象例:

- `manager_status_job`
- `patrol`

移行は段階的に進める。

### 移行ステップ

1. 現行 plist の用途を `spawn` か `send` に分類する
2. 既存コマンド文字列を、構造化 job 定義へ分解する
3. `jobs` レコードを登録する
4. `agentctl job scheduler once` で due 判定と action 解決を確認する
5. 親 `agentctl-scheduler.plist` を load する
6. 旧 plist を unload する
7. `job_runs` 履歴で期待通りに `spawn` / `send` されることを確認する

### 移行マッピング例

#### `patrol`

もし常駐 patrol session に定期依頼する設計なら、`send` job に置き換える。

```bash
agentctl job add patrol \
  --schedule '0 8 * * *' \
  --action send \
  --session patrol-codex \
  --instruction '今日の巡回をしてください'
```

#### 比較レポート更新

もし新しい worker を起動してレポート更新と PR 作成まで任せるなら、`spawn` job に置き換える。

```bash
agentctl job add weekly-report \
  --schedule '0 9 * * 1' \
  --action spawn \
  --repo chaspy/myassistant \
  --agent codex \
  --instruction '比較レポートを更新してPRを出してください'
```

### 移行時の注意点

- 旧 plist と新 scheduler の同時有効化は避ける
- command 文字列をそのまま持ち込まず、job の意図を `spawn` / `send` / `state-sync` などに再モデリングする
- まず `send` 系の read-mostly な job から移行すると安全

## 実装フェーズ分割案

### Milestone 1: 構造化 job 基盤

範囲:

- `jobs`, `job_runs`, `job_locks` migration
- `action=spawn` / `action=send` / `action=state-sync` のバリデーション
- `agentctl job add/list/delete/run`
- `agentctl job scheduler once/run`
- `forbid` のみ実装

完了条件:

- `spawn` job と `send` job を 1 件ずつ登録して実行できる
- run 履歴に instruction と結果が残る
- shell command を一切持たない

### Milestone 2: 運用機能

範囲:

- `show/update/enable/disable/history/logs`
- `catch_up_policy`
- 入力検証エラーの可視化
- README / docs 整備

完了条件:

- 日常運用が CLI だけで完結する
- instruction と実行結果を追跡できる

### Milestone 3: 既存 job の移行

範囲:

- 既存 plist を `spawn` / `send` job に変換
- 親 scheduler へ統合
- 旧 plist 廃止

完了条件:

- 実運用ジョブが `agentctl` の構造化 job に置き換わる

## Open Questions

1. `spawn` 時の `agent` は `auto` を default にするか
2. `spawn` job の `branch` 命名を固定規約にするか
3. `send` 実行時の `--no-wait` / `--verify` 相当を job 定義に持たせるか
4. `job logs` は action log ベースで十分か、それとも別途詳細ログが必要か
5. `manager_status_job` のような「単なる状態確認コマンド」は `command` として残し、agentctl 内部処理に切り出せる部分は `state-sync` のような専用 action に移す

## 推奨方針

- job action は structured action を優先し、`spawn` / `send` / `state-sync` を first-class に扱う
- `instruction` を job 定義の主役として持つ
- shell command は legacy deterministic script の移行用に限定する
- 初期 concurrency は `forbid` を default にする
- `spawn` の `agent` default は `auto`
- `catch_up_policy` default は `none`

## まとめ

job scheduler は cron の代替ではなく、`agentctl` の定期オーケストレーション機能として設計するべきである。したがって job 定義は shell command ではなく、`spawn` / `send` を構造化したデータとして持つ。

この設計により、`agentctl` は shell 実行基盤ではなく「定期的に agent を起動し、既存 agent に指示を送るオーケストレータ」として意味を持てる。レビュー後はこの方針で実装フェーズへ進む。
