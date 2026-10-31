CREATE TABLE IF NOT EXISTS launchboard_ideas (
 id SERIAL PRIMARY KEY,
 seed_key TEXT UNIQUE,
 title VARCHAR(100) NOT NULL,
 description VARCHAR(600) NOT NULL,
 category TEXT NOT NULL CHECK (category IN ('Product', 'Design', 'Engineering')),
 status TEXT NOT NULL DEFAULT 'idea' CHECK (status IN ('idea', 'building', 'shipped')),
 author VARCHAR(30) NOT NULL,
 seed_votes INTEGER NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS launchboard_votes (
 idea_id INTEGER NOT NULL REFERENCES launchboard_ideas(id) ON DELETE CASCADE,
 visitor TEXT NOT NULL,
 PRIMARY KEY (idea_id, visitor)
);
INSERT INTO launchboard_ideas (seed_key,title,description,category,status,author,seed_votes,created_at) VALUES
 ('command','どこからでも、⌘K。','プロジェクトも、設定も、最近の作業も。キーボードから一瞬でアクセスできるコマンドパレット。','Product','idea','Mika',42,NOW()-INTERVAL '6 days'),
 ('themes','気分に合わせて、ワークスペースも。','集中したい朝はライト、アイデアが広がる夜はダーク。あなたらしいテーマで作業しよう。','Design','idea','Ren',28,NOW()-INTERVAL '5 days'),
 ('onboarding','はじめの一歩を、もっと小さく。','初回ログインから最初のデプロイまでを、3ステップで案内するインタラクティブガイド。','Product','idea','Yuki',19,NOW()-INTERVAL '4 days'),
 ('preview','そのブランチ、URLで見せよう。','プルリクエストごとにプレビュー環境を。つくっているものを、チームですぐに試せるように。','Engineering','building','Haru',36,NOW()-INTERVAL '3 days'),
 ('activity','チームの動きが、ひと目で。','デプロイや更新をひとつのタイムラインに。離れていても、同じプロダクトをつくっている実感を。','Product','building','Sora',24,NOW()-INTERVAL '2 days'),
 ('deploy','アイデアから、公開まで。','GitHubをつないで、データベースを選んで、デプロイ。インフラの準備より、つくることに時間を使おう。','Engineering','shipped','Dafu',58,NOW()-INTERVAL '1 day'),
 ('mobile','小さな画面でも、大きなアイデアを。','スマートフォンでも投稿と投票が快適に。ひらめいたその瞬間を、取りこぼさない。','Design','shipped','Aoi',31,NOW()-INTERVAL '12 hours')
ON CONFLICT (seed_key) DO NOTHING;
