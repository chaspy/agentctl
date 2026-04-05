# Session Data Model Redesign

**Author**: Takeshi Kondo  
**Date**: 2026-04-06  
**Status**: Draft – for review before implementation

---

## 1. 現状分析

### 1.1 現在の sessions テーブルスキーマ

`internal/store/migrate.go` に定義されている現行スキーマ（V1〜V12 マイグレーション適用後）:

```sql
CREATE TABLE sessions (
    id             TEXT PRIMARY KEY,
    agent          TEXT NOT NULL,
    repository     TEXT NOT NULL,
    session_id     TEXT NOT NULL,
    cwd            TEXT NOT NULL DEFAULT '',
    git_branch     TEXT NOT NULL DEFAULT '',
    zellij_session TEXT NOT NULL DEFAULT '',   -- zellij セッション名
    status         TEXT NOT NULL DEFAULT 'unknown',  -- JSONL 由来のエージェント状態
    blocked_reason TEXT NOT NULL DEFAULT '',
    alive          INTEGER NOT NULL DEFAULT 0,  -- ★ 問題のフィールド
    last_message   TEXT NOT NULL DEFAULT '',
    last_role      TEXT NOT NULL DEFAULT '',
    last_active    TIMESTAMP,
    pr_number      INTEGER,
    pr_url         TEXT NOT NULL DEFAULT '',
    pr_state       TEXT NOT NULL DEFAULT '',
    task_summary   TEXT NOT NULL DEFAULT '',
    role           TEXT NOT NULL DEFAULT 'worker',
    archived       INTEGER NOT NULL DEFAULT 0,
    is_loop        INTEGER NOT NULL DEFAULT 0,
    permission_level INTEGER NOT NULL DEFAULT 1,
    runtime_status TEXT NOT NULL DEFAULT 'gone',  -- V11 で追加
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions_archive (
    -- sessions と同一構造 + archived_at TIMESTAMP
);
```

### 1.2 各フィールドの責務と現状の意味

| フィールド | 型 | 更新者 | 意味 |
|---|---|---|---|
| `alive` | INTEGER(0/1) | spawn, kill, **syncRuntimeStatus** | セッションが生きているか（意図と実態が混在） |
| `runtime_status` | TEXT | syncRuntimeStatus | zellij の観測状態: `running` / `exited` / `gone` |
| `status` | TEXT | JSONL enrichment | エージェントの作業状態: `active` / `idle` / `blocked` / `error` / `dead` |
| `archived` | INTEGER(0/1) | ArchiveSession, UnarchiveSession | 表示から隠す soft-delete フラグ（alive とは直交） |

### 1.3 alive / runtime_status / archived の関係と問題点

現在の `alive` は以下の 2 つの意味を兼ねてしまっている:

1. **spawn/kill の意思**: 「このセッションは稼働させる意図があるか」
2. **zellij の実態**: 「今この瞬間 zellij で確認できるか」

これが `syncRuntimeStatus()` の実装（`cmd/state_sync.go:403-475`）に現れている:

```go
// zellij_session が空 → alive = 0 にする
db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)

// zellij に見つからない → alive = 0 にする  
db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)

// zellij で exited → alive = 0 にする
db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)
```

その後 `syncSessionsToDB()` の末尾で `ArchiveDeadSessions()` が呼ばれ、`alive=0` のセッションが全て `sessions_archive` に移動する:

```go
// cmd/state_sync.go:197-199
if archived, err := store.ArchiveDeadSessions(db); err == nil && archived > 0 {
    fmt.Printf("Auto-archived %d dead/error session(s)\n", archived)
}
```

`ArchiveDeadSessions()` は `alive = 0` であれば **無条件に** sessions_archive へ移動する（`internal/store/sessions.go:323-351`）:

```go
tx.Exec(`INSERT OR REPLACE INTO sessions_archive ...
    SELECT ... FROM sessions WHERE alive = 0`)  // 条件は alive=0 のみ
tx.Exec("DELETE FROM sessions WHERE alive = 0")
```

### 1.4 繰り返し発生しているバグの根本原因

#### バグ 1: mass-archive（全セッション消滅）

`syncRuntimeStatus()` は `zellij list-sessions --no-formatting` の出力を使う。  
zellij がエラーや空リストを返すと:

```go
// cmd/state_sync.go:407-411
zellijSessions, err := listZellijDetailed()
if err != nil {
    fmt.Fprintf(os.Stderr, "warning: ...") 
    return  // ←ここで early return するが、エラーが err==nil で空リストを返す場合は通過してしまう
}
```

空リスト (`[]`) が返った場合は early return されず、全 alive セッションを `gone` として `alive=0` にする。  
直後の `ArchiveDeadSessions()` が全セッションを archive に移動してしまう。

#### バグ 2: zellij_session 未設定セッションの即死

`syncRuntimeStatus()` は `zellij_session` が空のセッションを即座に dead 扱いする（`state_sync.go:427-430`）:

```go
if zellijName == "" {
    db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)
    continue
}
```

spawn 直後など zellij_session が未設定の期間があると、次の sync で即座に archive される。

#### バグ 3: 復旧困難

sessions_archive に移動すると、`agentctl` コマンドからは復旧できない。  
手動 SQL (`INSERT INTO sessions SELECT ... FROM sessions_archive WHERE id = ?; DELETE FROM sessions_archive WHERE id = ?`) が必要。

---

## 2. 先行事例との比較

agentctl はセッション（Worker エージェント）のライフサイクルを管理するオーケストレーションシステムである。  
同様の課題を解決した先行システムの設計から学ぶ。

### 2.1 Kubernetes: desired state と actual state の分離

Kubernetes は Pod の状態を 2 層で管理する:

| 層 | フィールド | 意味 | 更新者 |
|---|---|---|---|
| Desired state | `spec.replicas`, `spec.containers` | 「こうあってほしい」という宣言 | ユーザー / コントローラー |
| Actual state | `status.phase`, `status.conditions` | 「今こうなっている」という観測 | kubelet / API server |

**「観測できない ≠ 死んでいる」の実現方法:**

Kubernetes は Node が unreachable になっても Pod をすぐに削除しない。  
`node.kubernetes.io/unreachable` taint が付与されてから `tolerationSeconds`（デフォルト 300秒）経過後に初めて Eviction が発生する。

```
Node unreachable → taint 付与 → 一定時間待機 → Pod Eviction
                  ↑ "観測できない"             ↑ "死んでいると確定"
```

**agentctl への示唆:**
- `alive` ← `spec` 相当（spawn/kill だけが変更）
- `runtime_status` ← `status` 相当（sync が観測して更新）
- runtime_status が `gone` になっても、すぐに alive=false にしない
- 一定時間 `gone` が継続してから「dead 確定」とする

### 2.2 OpenStack Nova: vm_state / task_state / power_state の 3 層管理

OpenStack Nova は VM インスタンスの状態を 3 つのフィールドで管理する:

| フィールド | 意味 | 例 |
|---|---|---|
| `vm_state` | 安定した状態（desired state に近い） | `active`, `stopped`, `error` |
| `task_state` | 進行中の遷移タスク | `spawning`, `deleting`, `rebooting`, `NULL` |
| `power_state` | ハイパーバイザーの観測値（actual state） | `running`, `shutdown`, `nostate` |

**「観測できない ≠ 死んでいる」の実現方法:**

Nova のハイパーバイザーが `nostate`（観測不能）を返した場合、`vm_state` は変更されない。  
`power_state` のみが `nostate` に更新され、`task_state` に `power-update-pending` がセットされる。  
一定時間 `nostate` が継続し、かつ `vm_state` が `active` である場合に「エラー状態」と判定する。

```
power_state=nostate → task_state=power-update-pending → 待機
                    ↑ "観測不能"                         ↑ 再試行
→ 複数回失敗 → vm_state=error
             ↑ "確定的な問題"
```

**agentctl への示唆:**
- `alive`（意思） / `runtime_status`（観測） の 2 層分離は Nova の `vm_state` / `power_state` と対応する
- `task_state` に相当する「遷移中」状態（spawning, killing）を持つことで、`zellij_session` 未設定期間の誤検知を防げる

### 2.3 共通原則の抽出

これらのシステムが共通して採用している原則:

1. **意思と実態の分離**: 「こうあってほしい（spec/desired）」と「今こうなっている（status/actual）」は別フィールド
2. **観測者は意思フィールドを変更しない**: kubelet は `spec` を変えない。Nova の power monitor は `vm_state` を変えない
3. **観測不能は一時的なエラーとして扱う**: 即座に「死んでいる」と判定しない
4. **遷移状態の明示**: spawning / deleting など中間状態を持ち、その間は誤検知しない
5. **確定的な削除には複数条件**: 時間経過 + 複数回の観測失敗 + 意思の確認

---

## 3. 新しいデータモデルの提案

### 3.1 設計原則

上記の先行事例を踏まえ、以下の原則を採用する:

**原則 1: alive は「意思」、runtime_status は「実態」として完全分離**
- `alive = true` にできるのは `spawn` コマンドのみ
- `alive = false` にできるのは `kill` コマンドのみ
- `sync`（`syncRuntimeStatus`）は `alive` を **一切変更しない**

**原則 2: 観測できない ≠ 死んでいる**
- zellij が空リストを返しても `alive` は変わらない
- `runtime_status = gone` が継続した場合でも、即座に archive しない
- archive 条件は「`alive = false` かつ一定期間経過」のみ

**原則 3: 遷移状態の明示**
- spawn 中（zellij セッション作成前）は `lifecycle_state = spawning`
- kill 中は `lifecycle_state = killing`
- この状態では `syncRuntimeStatus` の dead 判定をスキップする

### 3.2 sessions テーブルの変更案

```sql
-- 変更点のみ記載

-- 追加: ライフサイクル遷移状態（spawning / running / killing / stopped）
ALTER TABLE sessions ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'running';

-- alive の意味を「意思」に限定（変更なし、ただし更新者を制限）
-- alive = 1: spawn された（kill されるまで 1 を保つ）
-- alive = 0: kill された（またはユーザーが手動で無効化した）

-- runtime_status の意味を明確化（変更なし、ただし alive への影響を削除）
-- runtime_status = 'running': zellij で確認できた
-- runtime_status = 'exited': zellij に存在するが EXITED 状態
-- runtime_status = 'gone': zellij に存在しない（観測不能を含む）
-- runtime_status = 'unknown': まだ確認していない

-- 追加: runtime_status が gone/exited になった最初の時刻
ALTER TABLE sessions ADD COLUMN runtime_gone_since TIMESTAMP;

-- archive テーブルへの移動条件を判定するため
-- alive = 0 に設定された時刻（kill コマンドが書く）
ALTER TABLE sessions ADD COLUMN killed_at TIMESTAMP;
```

#### lifecycle_state の遷移図

```
[spawn 開始]
    ↓
lifecycle_state = 'spawning'
    ↓ zellij セッション作成完了・alive=true 設定
lifecycle_state = 'running'
    ↓ kill コマンド実行
lifecycle_state = 'killing'
    ↓ zellij セッション削除完了・alive=false 設定
lifecycle_state = 'stopped'
    ↓ killed_at から TTL 経過
[sessions_archive へ移動]
```

### 3.3 sessions_archive との関係整理

**方針: archive テーブルは維持するが、移動条件を厳格化する**

現在: `alive = 0` であれば無条件に移動  
変更後: 以下の **全条件を満たす** 場合のみ移動

```sql
-- ArchiveDeadSessions の新しい条件
WHERE alive = 0                                    -- kill された
  AND lifecycle_state = 'stopped'                  -- kill 完了
  AND killed_at < datetime('now', '-1 hour')       -- 1時間以上経過
```

TTL（1時間）を設ける理由:
- kill 直後に archive すると `agentctl resume` が動作しない期間が生まれる
- 誤 kill のリカバリー時間を確保する
- Kubernetes の `tolerationSeconds` に相当する猶予期間

**廃止を検討しない理由:**
- archive テーブルはデバッグ・振り返りに有用
- sessions テーブルに全履歴を残すとクエリ性能が劣化する

### 3.4 マイグレーション方針

```sql
-- V13: lifecycle_state, runtime_gone_since, killed_at の追加
ALTER TABLE sessions ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'running';
ALTER TABLE sessions ADD COLUMN runtime_gone_since TIMESTAMP;
ALTER TABLE sessions ADD COLUMN killed_at TIMESTAMP;

ALTER TABLE sessions_archive ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'stopped';
ALTER TABLE sessions_archive ADD COLUMN runtime_gone_since TIMESTAMP;
ALTER TABLE sessions_archive ADD COLUMN killed_at TIMESTAMP;

-- 既存データの補正: alive=0 のセッションは stopped として扱う
UPDATE sessions SET lifecycle_state = 'stopped', killed_at = updated_at WHERE alive = 0;
```

---

## 4. sync ロジックの変更案

### 4.1 syncRuntimeStatus の変更点

**現行の問題箇所** (`cmd/state_sync.go:424-441`):

```go
// 現行: alive を直接変更してしまう
for _, s := range aliveSessions {
    if zellijName == "" {
        db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)  // ← 削除
        continue
    }
    if zs.exited {
        db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)  // ← 削除
    } else {
        db.Exec("UPDATE sessions SET runtime_status = 'running', ...", s.ID)
    }
    // found=false の場合
    db.Exec("UPDATE sessions SET alive = 0, status = 'dead', ...", s.ID)  // ← 削除
}
```

**変更後**:

```go
func syncRuntimeStatus(db *sql.DB) {
    zellijSessions, err := listZellijDetailed()
    if err != nil {
        fmt.Fprintf(os.Stderr, "warning: could not list zellij sessions: %v (skipping runtime status update)\n", err)
        return
    }

    // ★ 空リストの場合は観測失敗として早期リターン（全消滅を防ぐ）
    if len(zellijSessions) == 0 {
        fmt.Fprintf(os.Stderr, "warning: zellij returned empty session list, skipping runtime status update\n")
        return
    }

    zellijMap := buildZellijMap(zellijSessions)

    aliveSessions, _ := store.ListSessionsByAlive(db, true)
    for _, s := range aliveSessions {
        // spawning 中はスキップ（zellij_session が未設定の正常期間）
        if s.LifecycleState == "spawning" {
            continue
        }

        zellijName := s.ZellijSession
        if zellijName == "" {
            // spawning 以外で zellij_session が空 → gone 扱いだが alive は変えない
            updateRuntimeStatus(db, s.ID, "gone")
            continue
        }

        if zs, found := zellijMap[strings.ToLower(zellijName)]; found {
            if zs.exited {
                updateRuntimeStatus(db, s.ID, "exited")
            } else {
                db.Exec("UPDATE sessions SET runtime_status = 'running', runtime_gone_since = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
            }
        } else {
            updateRuntimeStatus(db, s.ID, "gone")
        }
    }
}

// updateRuntimeStatus は runtime_status を gone/exited に更新する。
// runtime_gone_since は初回のみセットする（連続観測の開始時刻を保持）。
// alive は変更しない。
func updateRuntimeStatus(db *sql.DB, id, status string) {
    db.Exec(`UPDATE sessions SET
        runtime_status = ?,
        runtime_gone_since = COALESCE(runtime_gone_since, CURRENT_TIMESTAMP),
        updated_at = CURRENT_TIMESTAMP
        WHERE id = ?`, status, id)
}
```

### 4.2 ArchiveDeadSessions の条件変更

**現行** (`internal/store/sessions.go:323-351`):

```go
// alive = 0 なら無条件にアーカイブ
SELECT ... FROM sessions WHERE alive = 0
```

**変更後**:

```go
// 条件: kill 完了 かつ killed_at から TTL 経過
func ArchiveDeadSessions(db *sql.DB) (int, error) {
    tx, err := db.Begin()
    // ...
    result, err := tx.Exec(`INSERT OR REPLACE INTO sessions_archive ...
        SELECT ... FROM sessions
        WHERE alive = 0
          AND lifecycle_state = 'stopped'
          AND killed_at < datetime('now', '-1 hour')`)
    // ...
    tx.Exec(`DELETE FROM sessions
        WHERE alive = 0
          AND lifecycle_state = 'stopped'
          AND killed_at < datetime('now', '-1 hour')`)
}
```

### 4.3 kill コマンドの変更

`alive = false` を設定する際に `lifecycle_state` と `killed_at` も更新する:

```go
// cmd/kill.go 変更後
db.Exec(`UPDATE sessions SET
    alive = 0,
    lifecycle_state = 'stopped',
    killed_at = CURRENT_TIMESTAMP,
    runtime_status = 'gone',
    status = 'dead',
    updated_at = CURRENT_TIMESTAMP
    WHERE zellij_session = ?`, zellijSession)
```

### 4.4 spawn コマンドの変更

zellij セッション作成前後で `lifecycle_state` を更新する:

```go
// zellij セッション作成前
store.UpsertSession(db, &store.Session{
    // ...
    Alive:          true,
    LifecycleState: "spawning",  // 新規追加
})

// zellij セッション作成完了後
db.Exec(`UPDATE sessions SET
    lifecycle_state = 'running',
    zellij_session = ?,
    updated_at = CURRENT_TIMESTAMP
    WHERE id = ?`, sessionName, sessionID)
```

### 4.5 後方互換性の考慮

- `alive` フィールド自体は残すため、既存の `ListSessionsByAlive()` などは変更不要
- `lifecycle_state` はデフォルト `running` のため、既存の alive=1 セッションは正常動作
- `killed_at` は NULL の場合、archive 条件（`killed_at < datetime('now', '-1 hour')`）を満たさないため安全
  - ただし alive=0 かつ killed_at=NULL のレコードが archive されなくなるため、V13 マイグレーションで補正が必要

---

## 5. リスク・トレードオフ

### 5.1 変更の難易度・影響範囲

| 変更箇所 | 難易度 | 影響 |
|---|---|---|
| V13 マイグレーション追加 | 低 | DB スキーマ変更（後方互換あり） |
| `syncRuntimeStatus` の alive 更新削除 | 低 | 最重要変更。既存テストの修正が必要 |
| `ArchiveDeadSessions` の条件追加 | 低 | archive 頻度が下がる（意図通り） |
| `kill` コマンドの `killed_at` 追加 | 低 | 追加のみ |
| `spawn` コマンドの `lifecycle_state` 追加 | 中 | spawning→running の遷移タイミング調整が必要 |
| `Session` 構造体への `LifecycleState` 追加 | 低 | Go 構造体の変更 |
| 既存の alive=0 データの補正 | 中 | `killed_at = updated_at` とするが不正確な場合あり |

### 5.2 新たに発生しうる問題

**問題 1: runtime_gone_since が溜まり続ける**
- alive=1 かつ runtime_status=gone のセッションは archive されなくなる
- 対策: `agentctl list` に `runtime_gone_since` を表示し、ユーザーが `agentctl kill` を呼べるようにする
- または: `runtime_gone_since > 24時間` の場合はアラート表示する

**問題 2: kill し忘れた場合のゾンビセッション**
- alive=1 のまま zellij から消えたセッションは archive されない
- 対策: `agentctl list` でユーザーに明示的に `kill` を促す。`runtime_status=gone` かつ `runtime_gone_since > N時間` のセッションに警告表示する

**問題 3: resume コマンドとの整合性**
- 現在 `agentctl resume` は sessions_archive からセッションを復元する想定
- alive=0 のセッションが sessions テーブルに残る期間が生まれるため、resume の対象テーブルを確認・調整する必要がある

### 5.3 段階的移行の提案

**Phase 1（優先）: 空リスト early return の追加**

最も重大な mass-archive バグの修正。リスクが低く即効性が高い。

```go
if len(zellijSessions) == 0 {
    fmt.Fprintf(os.Stderr, "warning: zellij returned empty session list, skipping\n")
    return
}
```

**Phase 2: `syncRuntimeStatus` から alive 変更を除去**

`runtime_status` のみを更新するように変更。合わせて `runtime_gone_since` フィールドを追加。

**Phase 3: `ArchiveDeadSessions` の条件強化**

`killed_at` を kill コマンドで設定し、TTL 付きの archive 条件に変更。

**Phase 4: `lifecycle_state` の導入**

spawn 中の誤検知を防ぐための `spawning` 状態の追加。

---

## 6. まとめ

### 変更前後の比較

| 問題 | 変更前 | 変更後 |
|---|---|---|
| mass-archive | zellij が空リストを返すと全消滅 | 空リストは観測失敗として無視 |
| alive の意味 | 意思と実態が混在 | 意思（spawn/kill のみ変更）と実態（runtime_status）を分離 |
| zellij_session 未設定の即死 | spawning 中でも immediately dead | lifecycle_state=spawning の間はスキップ |
| 復旧困難 | archive に移動したら手動 SQL のみ | TTL 期間中は sessions テーブルに残り `agentctl resume` で復旧可能 |

### 実装優先度

```
Phase 1 > Phase 2 > Phase 3 > Phase 4
（空リスト対策） （alive分離） （TTL条件） （lifecycle_state）
```

Phase 1 単独でも mass-archive の頻度を大幅に下げられるため、レビュー後に分割実装を推奨する。
