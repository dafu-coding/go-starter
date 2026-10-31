package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type TaskFields struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Author      string `json:"author"`
	Assignee    string `json:"assignee"`
	Priority    string `json:"priority"`
	DueDate     string `json:"dueDate"`
}

type TaskPatch struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Category    *string `json:"category"`
	Status      *string `json:"status"`
	Assignee    *string `json:"assignee"`
	Priority    *string `json:"priority"`
	DueDate     *string `json:"dueDate"`
	Archived    *bool   `json:"archived"`
	Version     int     `json:"version"`
}

func validDate(value string) bool {
	if value == "" {
		return true
	}
	date, err := time.Parse("2006-01-02", value)
	return err == nil && date.Year() >= 1900 && date.Year() <= 9999
}
func validPriority(value string) bool { return value == "high" || value == "normal" || value == "low" }
func validStatus(value string) bool {
	return value == "idea" || value == "building" || value == "shipped"
}
func taskID(r *http.Request) (int, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 || id > 2147483647 {
		return 0, fmt.Errorf("invalid task ID")
	}
	return id, nil
}
func clean(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}
func validText(value *string, min, max int) bool {
	return value == nil || (utf8.RuneCountInString(*value) >= min && utf8.RuneCountInString(*value) <= max)
}

func listIdeas(ctx context.Context, db *sql.DB, visitor string) ([]Idea, error) {
	rows, err := db.QueryContext(ctx, `SELECT i.id,i.title,i.description,i.category,i.status,i.author,
 (SELECT COUNT(*) FROM launchboard_votes v WHERE v.idea_id=i.id),
 EXISTS(SELECT 1 FROM launchboard_votes v WHERE v.idea_id=i.id AND v.visitor=$1),
 i.created_at,i.assignee,i.priority,COALESCE(TO_CHAR(i.due_date,'YYYY-MM-DD'),''),i.archived,i.version,i.updated_at,i.seed_key IS NOT NULL
 FROM launchboard_ideas i ORDER BY i.created_at DESC,i.id DESC`, visitor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ideas := []Idea{}
	for rows.Next() {
		var idea Idea
		if err := rows.Scan(&idea.ID, &idea.Title, &idea.Description, &idea.Category, &idea.Status, &idea.Author, &idea.Votes, &idea.Voted, &idea.CreatedAt, &idea.Assignee, &idea.Priority, &idea.DueDate, &idea.Archived, &idea.Version, &idea.UpdatedAt, &idea.Sample); err != nil {
			return nil, err
		}
		ideas = append(ideas, idea)
	}
	return ideas, rows.Err()
}

func createTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var body TaskFields
	if !decode(w, r, &body) {
		return
	}
	clean(&body.Title)
	clean(&body.Description)
	clean(&body.Author)
	clean(&body.Assignee)
	if body.Author == "" {
		body.Author = "Guest"
	}
	if body.Priority == "" {
		body.Priority = "normal"
	}
	if !validText(&body.Title, 1, 100) || !validText(&body.Description, 0, 3000) || !validText(&body.Author, 1, 30) || !validText(&body.Assignee, 0, 60) || !validCategory(body.Category) || !validPriority(body.Priority) || !validDate(body.DueDate) {
		fail(w, 400, "タイトル・担当者・期限を確認してください。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id int
	err := db.QueryRowContext(ctx, `INSERT INTO launchboard_ideas(title,description,category,author,assignee,priority,due_date) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::date) RETURNING id`, body.Title, body.Description, body.Category, body.Author, body.Assignee, body.Priority, body.DueDate).Scan(&id)
	if err != nil {
		fail(w, 503, "保存できませんでした。もう一度お試しください。")
		return
	}
	respond(w, 201, map[string]int{"id": id, "version": 1})
}

func updateTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	id, err := taskID(r)
	if err != nil {
		fail(w, 400, "Invalid task ID")
		return
	}
	var body TaskPatch
	if !decode(w, r, &body) {
		return
	}
	clean(body.Title)
	clean(body.Description)
	clean(body.Assignee)
	if body.Version < 1 || !validText(body.Title, 1, 100) || !validText(body.Description, 0, 3000) || !validText(body.Assignee, 0, 60) || (body.Category != nil && !validCategory(*body.Category)) || (body.Status != nil && !validStatus(*body.Status)) || (body.Priority != nil && !validPriority(*body.Priority)) || (body.DueDate != nil && !validDate(*body.DueDate)) {
		fail(w, 400, "入力内容とバージョンを確認してください。")
		return
	}
	if body.Title == nil && body.Description == nil && body.Category == nil && body.Status == nil && body.Assignee == nil && body.Priority == nil && body.DueDate == nil && body.Archived == nil {
		fail(w, 400, "変更する項目がありません。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var version int
	err = db.QueryRowContext(ctx, `UPDATE launchboard_ideas SET
 title=COALESCE($1,title),description=COALESCE($2,description),category=COALESCE($3,category),
 assignee=COALESCE($4,assignee),priority=COALESCE($5,priority),
 due_date=CASE WHEN $6 THEN NULLIF($7,'')::date ELSE due_date END,
 completed_at=CASE WHEN $8::text='shipped' AND status<>'shipped' THEN NOW() WHEN $8::text IS NOT NULL AND $8::text<>'shipped' THEN NULL ELSE completed_at END,
 status=COALESCE($8,status),archived=COALESCE($9,archived),version=version+1,updated_at=NOW()
 WHERE id=$10 AND version=$11 RETURNING version`, body.Title, body.Description, body.Category, body.Assignee, body.Priority, body.DueDate != nil, body.DueDate, body.Status, body.Archived, id, body.Version).Scan(&version)
	if err == sql.ErrNoRows {
		var exists bool
		if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM launchboard_ideas WHERE id=$1)", id).Scan(&exists); err != nil {
			fail(w, 503, "更新できませんでした。")
			return
		}
		if !exists {
			fail(w, 404, "タスクが見つかりません。")
			return
		}
		fail(w, 409, "別の画面で更新されています。閉じて最新の内容を確認してください。")
		return
	}
	if err != nil {
		fail(w, 503, "更新できませんでした。もう一度お試しください。")
		return
	}
	respond(w, 200, map[string]int{"id": id, "version": version})
}

func exportTasks(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tasks, err := listIdeas(ctx, db, "")
	if err != nil {
		fail(w, 503, "エクスポートできませんでした。")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="launchboard-tasks.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	writer.Write([]string{"ID", "タイトル", "説明", "カテゴリ", "ステータス", "担当者", "優先度", "期限", "アーカイブ", "作成日時"})
	for _, task := range tasks {
		row := []string{strconv.Itoa(task.ID), task.Title, task.Description, task.Category, map[string]string{"idea": "未着手", "building": "進行中", "shipped": "完了"}[task.Status], task.Assignee, map[string]string{"high": "高", "normal": "通常", "low": "低"}[task.Priority], task.DueDate, map[bool]string{true: "はい", false: "いいえ"}[task.Archived], task.CreatedAt.Format(time.RFC3339)}
		for i, value := range row {
			row[i] = csvSafe(value)
		}
		writer.Write(row)
	}
	writer.Flush()
}
func csvSafe(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsAny(trimmed[:1], "=+-@") {
		return "'" + value
	}
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") {
		return "'" + value
	}
	return value
}
