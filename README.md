# Launchboard ↗

小さなひらめきを、次のリリースへ。Go + PostgreSQL で動く、チームのアイデア・投票・ロードマップボードです。

## できること

- アイデアの投稿（タイトル、説明、カテゴリ、名前）
- ブラウザごとの投票・取り消し（DBの一意制約で重複を防止）
- 「アイデア → つくっている → できた！」のステータス変更
- カテゴリ絞り込み、キーワード検索、人気順・新着順
- 自分が投票したアイデア、リリース済みの一覧
- 実DBの状態を返す `/health`、30秒ごとの画面更新
- レスポンシブ画面、キーボード操作に対応した投稿ダイアログ

投稿・投票・ステータスは PostgreSQL に保存され、再起動しても維持されます。初回起動時に7件のアイデアとサンプル投票数を追加します。サンプル投票数と実際の投票数の合計を表示します。再デプロイで既存データやステータスはリセットされません。

公開デモ用です。ログインなしで全員が投稿・投票・ステータス変更できます。投票はブラウザCookie単位で、本人認証ではありません。機密情報を投稿しないでください。

## Kamui へのデプロイ

GitHub の `dafu-coding/go-starter`、ブランチ `main` を選択し、PostgreSQL データベースを接続してください。`DATABASE_URL` は Kamui から自動設定されます。

| 項目 | 設定 |
| --- | --- |
| Setup Command | `go build -trimpath -ldflags='-s -w' -o out/app ./cmd/server && go build -trimpath -ldflags='-s -w' -o out/db ./cmd/db` |
| Pre-deploy Command | `out/db` |
| Start Command | `out/app` |
| Health Check Path | `/health` |

`PORT` は Kamui の設定を使用します（ローカル既定値は `8000`）。静的アセットとマイグレーションはバイナリに埋め込まれるため、作業ディレクトリに依存しません。サーバー起動時にもマイグレーションを実行します。複数レプリカの同時起動は PostgreSQL advisory lock で直列化します。

`DATABASE_URL` は標準の `postgres://` / `postgresql://` URLをそのまま利用します。接続先に応じたTLSパラメーターを使用してください。DB未接続時は起動に失敗します。

## ローカル起動

Go 1.23以降と PostgreSQL を用意してください。

```sh
export DATABASE_URL='postgres://localhost:5432/launchboard?sslmode=disable'
go run ./cmd/db
go run ./cmd/server
```

[http://localhost:8000](http://localhost:8000) を開きます。

## 検証

```sh
go test ./...
go vet ./...
node --check cmd/server/static/app.js
```

DB統合テストは、専用の使い捨てローカルDBで実行します。

```sh
createdb -h 127.0.0.1 -p 55439 -U launchboard launchboard_test
TEST_DATABASE_URL='postgres://launchboard@127.0.0.1:55439/launchboard_test?sslmode=disable' go test -race ./...
```

投稿、重複投票防止、投票取消、ステータス更新、再接続時の永続化、マイグレーションの再実行、入力検証、別Originからの書き込み拒否を確認します。`TEST_DATABASE_URL` 未設定時はDB統合テストをスキップします。

## デモの流れ（約1分）

1. アイデアボードと3つの集計を表示。
2. 「アイデアを追加」から好きなアイデアを投稿。
3. ↑ を押して投票。My votes に切り替えて確認。
4. カード下のステータスを「つくっている」、続けて「できた！」へ変更。
5. リロードしてデータが維持されることを確認。

既存スターターの `greetings` テーブルには変更を加えません。新しいデータは `launchboard_ideas` / `launchboard_votes` に保存します。
