INSERT INTO launchboard_ideas(seed_key,title,description,category,status,author,assignee,priority,due_date) VALUES
 ('sample-v2-brief','リリース範囲を決める',E'今回のリリースで届ける機能を3つに絞る。\n決めた内容をこのタスクのメモに追記する。','Product','idea','Sample','田中','high',CURRENT_DATE),
 ('sample-v2-design','トップページのデザインを確認','余白・見出し・スマートフォン表示を確認。気になる箇所はスクリーンショットを添えて共有する。','Design','building','Sample','佐藤','normal',CURRENT_DATE+2),
 ('sample-v2-test','フォームの動作テスト','入力エラー、送信成功、二重送信の3パターンを確認する。','Engineering','idea','Sample','田中','high',CURRENT_DATE-1),
 ('sample-v2-docs','使い方ガイドを用意','初めて使う人が迷わないように、作成・担当・期限・完了の流れをまとめる。','Product','building','Sample','鈴木','normal',CURRENT_DATE+3),
 ('sample-v2-deploy','検証環境を準備','GitHubとDBを接続し、アプリが起動するところまで確認する。','Engineering','shipped','Sample','鈴木','normal',CURRENT_DATE-2),
 ('sample-v2-icons','アイコンのサイズを揃える','ボタンやサイドバーのアイコンを見直す。','Design','idea','Sample','','low',NULL)
ON CONFLICT(seed_key) DO NOTHING;
