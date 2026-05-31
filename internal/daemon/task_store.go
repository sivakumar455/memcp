package daemon

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sivakumar455/memcp/internal/memory"

	"github.com/google/uuid"
)

// Task represents a queued daemon task persisted in SQLite.
type Task struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	SourceID     string    `json:"source_id"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Priority     string    `json:"priority"`
	Category     string    `json:"category"`
	Status       string    `json:"status"`
	RawData      string    `json:"raw_data,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ActedAt      time.Time `json:"acted_at,omitempty"`
	ActionResult string    `json:"action_result,omitempty"`
	SnoozedUntil time.Time `json:"snoozed_until,omitempty"`
}

// TaskFilter controls task listing queries.
type TaskFilter struct {
	Status   string
	Source   string
	Priority string
	Limit    int
}

// TaskCounts holds aggregate task counts by status.
type TaskCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Dismissed  int `json:"dismissed"`
	Snoozed    int `json:"snoozed"`
	Total      int `json:"total"`
}

// TaskStore manages daemon tasks in the SQLite database.
type TaskStore struct {
	db *sql.DB
}

// NewTaskStore creates a TaskStore backed by the given memory store's DB.
func NewTaskStore(store *memory.Store) *TaskStore {
	return &TaskStore{db: store.DB()}
}

func (ts *TaskStore) Create(t Task) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := memory.FormatDBTime(time.Now())
	_, err := ts.db.Exec(`
		INSERT INTO daemon_tasks (id, source, source_id, title, summary, priority, category, status, raw_data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.Source, t.SourceID, t.Title, t.Summary, t.Priority, t.Category, t.Status, t.RawData, now, now)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}

func (ts *TaskStore) Exists(source, sourceID string) (bool, error) {
	var count int
	err := ts.db.QueryRow(
		`SELECT COUNT(*) FROM daemon_tasks WHERE source = ? AND source_id = ? AND status NOT IN ('completed', 'dismissed')`,
		source, sourceID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (ts *TaskStore) Get(id string) (*Task, error) {
	row := ts.db.QueryRow(`
		SELECT id, source, source_id, title, summary, priority, category, status, raw_data,
		       created_at, updated_at, COALESCE(acted_at,''), COALESCE(action_result,''), COALESCE(snoozed_until,'')
		FROM daemon_tasks WHERE id = ?`, id)
	task, err := scanTask(row)
	if err != nil {
		return nil, err
	}
	if task != nil {
		return task, nil
	}
	return ts.GetBySourceID(id)
}

func (ts *TaskStore) GetBySourceID(sourceID string) (*Task, error) {
	row := ts.db.QueryRow(`
		SELECT id, source, source_id, title, summary, priority, category, status, raw_data,
		       created_at, updated_at, COALESCE(acted_at,''), COALESCE(action_result,''), COALESCE(snoozed_until,'')
		FROM daemon_tasks WHERE source_id = ? ORDER BY created_at DESC LIMIT 1`, sourceID)
	return scanTask(row)
}

func (ts *TaskStore) List(f TaskFilter) ([]Task, error) {
	q := `SELECT id, source, source_id, title, summary, priority, category, status, raw_data,
	             created_at, updated_at, COALESCE(acted_at,''), COALESCE(action_result,''), COALESCE(snoozed_until,'')
	      FROM daemon_tasks WHERE 1=1`
	var args []any

	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.Source != "" {
		q += " AND source = ?"
		args = append(args, f.Source)
	}
	if f.Priority != "" {
		q += " AND priority = ?"
		args = append(args, f.Priority)
	}

	q += " ORDER BY CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 WHEN 'low' THEN 3 END, created_at DESC"

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q += " LIMIT ?"
	args = append(args, limit)

	rows, err := ts.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var createdStr, updatedStr, actedStr, snoozedStr string
		if err := rows.Scan(&t.ID, &t.Source, &t.SourceID, &t.Title, &t.Summary, &t.Priority,
			&t.Category, &t.Status, &t.RawData,
			&createdStr, &updatedStr, &actedStr, &t.ActionResult, &snoozedStr); err != nil {
			return nil, err
		}
		t.CreatedAt = memory.ParseDBTime(createdStr)
		t.UpdatedAt = memory.ParseDBTime(updatedStr)
		t.ActedAt = memory.ParseDBTime(actedStr)
		t.SnoozedUntil = memory.ParseDBTime(snoozedStr)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (ts *TaskStore) UpdateStatus(id, status, result string) error {
	now := memory.FormatDBTime(time.Now())
	actedAt := ""
	if status == "completed" || status == "dismissed" {
		actedAt = now
	}
	_, err := ts.db.Exec(`
		UPDATE daemon_tasks SET status = ?, action_result = ?, acted_at = CASE WHEN ? != '' THEN ? ELSE acted_at END, updated_at = ?
		WHERE id = ?
	`, status, result, actedAt, actedAt, now, id)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	return nil
}

func (ts *TaskStore) Snooze(id string, until time.Time) error {
	now := memory.FormatDBTime(time.Now())
	_, err := ts.db.Exec(`
		UPDATE daemon_tasks SET status = 'snoozed', snoozed_until = ?, updated_at = ? WHERE id = ?
	`, memory.FormatDBTime(until), now, id)
	if err != nil {
		return fmt.Errorf("snooze task: %w", err)
	}
	return nil
}

func (ts *TaskStore) UnsnoozeReady() (int, error) {
	now := memory.FormatDBTime(time.Now())
	res, err := ts.db.Exec(`
		UPDATE daemon_tasks SET status = 'pending', updated_at = ? WHERE status = 'snoozed' AND snoozed_until <= ?
	`, now, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (ts *TaskStore) PendingCount() (int, error) {
	var count int
	err := ts.db.QueryRow(`SELECT COUNT(*) FROM daemon_tasks WHERE status = 'pending'`).Scan(&count)
	return count, err
}

func (ts *TaskStore) Counts() (TaskCounts, error) {
	var c TaskCounts
	rows, err := ts.db.Query(`SELECT status, COUNT(*) FROM daemon_tasks GROUP BY status`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if rows.Scan(&status, &count) == nil {
			switch status {
			case "pending":
				c.Pending = count
			case "in_progress":
				c.InProgress = count
			case "completed":
				c.Completed = count
			case "dismissed":
				c.Dismissed = count
			case "snoozed":
				c.Snoozed = count
			}
			c.Total += count
		}
	}
	return c, nil
}

func (ts *TaskStore) Cleanup(olderThan time.Duration) (int, error) {
	cutoff := memory.FormatDBTime(time.Now().Add(-olderThan))
	res, err := ts.db.Exec(`
		DELETE FROM daemon_tasks WHERE status IN ('completed', 'dismissed') AND updated_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PendingSummary returns a compact text summary of pending tasks for agent_recall.
func (ts *TaskStore) PendingSummary(limit int) (string, error) {
	tasks, err := ts.List(TaskFilter{Status: "pending", Limit: limit})
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", nil
	}

	count, _ := ts.PendingCount()
	result := fmt.Sprintf("--- Pending Tasks (%d) ---\n", count)
	result += "ACTION REQUIRED: Review these tasks queued by the background daemon.\n"
	result += "Inform the user about pending tasks. For each, the user can: investigate, dismiss, or snooze.\n"
	result += "Use agent_tasks(operation=\"get\", id=\"<id>\") for details, agent_task_action to act.\n\n"

	hasCritical := false
	for _, t := range tasks {
		age := time.Since(t.CreatedAt)
		ageStr := formatAge(age)
		marker := ""
		if t.Priority == "critical" {
			marker = " *** CRITICAL ***"
			hasCritical = true
		}
		result += fmt.Sprintf("- [%s] %s: %s (%s, %s)%s\n", t.Priority, t.SourceID, t.Title, t.Source, ageStr, marker)
	}

	if hasCritical {
		result += "\nURGENT: Critical-priority tasks detected. Notify the user immediately.\n"
	}
	return result, nil
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func scanTask(row *sql.Row) (*Task, error) {
	var t Task
	var createdStr, updatedStr, actedStr, snoozedStr string
	err := row.Scan(&t.ID, &t.Source, &t.SourceID, &t.Title, &t.Summary, &t.Priority,
		&t.Category, &t.Status, &t.RawData,
		&createdStr, &updatedStr, &actedStr, &t.ActionResult, &snoozedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.CreatedAt = memory.ParseDBTime(createdStr)
	t.UpdatedAt = memory.ParseDBTime(updatedStr)
	t.ActedAt = memory.ParseDBTime(actedStr)
	t.SnoozedUntil = memory.ParseDBTime(snoozedStr)
	return &t, nil
}
