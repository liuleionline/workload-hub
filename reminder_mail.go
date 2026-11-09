package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MailReminderResult struct {
	WeekEnd   string `json:"week_end"`
	Status    string `json:"status"`
	Missing   int    `json:"missing"`
	Sent      int    `json:"smtp_accepted"`
	Skipped   int    `json:"skipped"`
	Invalid   int    `json:"invalid_email"`
	Uncertain int    `json:"uncertain"`
}

type reminderDeliver func(context.Context, string, []byte) error

func runMailReminders(cfg Config, args []string) error {
	flags := flag.NewFlagSet("send-reminders", flag.ContinueOnError)
	send := flags.Bool("send", false, "实际发送；默认只模拟，不发送、不记录尝试")
	checkAuth := flags.Bool("check-auth", false, "仅验证SMTP授权，不发送邮件")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*send && *checkAuth) {
		return errors.New("命令参数无效")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if *checkAuth {
		client, err := newReminderSMTP(ctx)
		if err != nil {
			return err
		}
		client.close()
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "smtp_authenticated", "mail": "not_sent"})
	}
	db, err := openReminderReadOnlyDB(cfg.DatabasePath)
	if err != nil {
		return errors.New("业务数据库只读连接失败")
	}
	defer db.Close()
	app := &App{cfg: cfg, db: db}
	var state *sql.DB
	if *send {
		statePath := envOr("APP_REMINDER_STATE_DB", "")
		if statePath == "" {
			return errors.New("必须配置独立发信记录路径")
		}
		source, _ := filepath.Abs(cfg.DatabasePath)
		target, _ := filepath.Abs(statePath)
		if source == target {
			return errors.New("发信记录不得使用业务数据库")
		}
		if sourceInfo, e1 := os.Stat(source); e1 == nil {
			if targetInfo, e2 := os.Stat(target); e2 == nil && os.SameFile(sourceInfo, targetInfo) {
				return errors.New("发信记录不得指向业务数据库")
			}
		}
		state, err = openReminderMailState(statePath)
		if err != nil {
			return errors.New("发信记录打开失败")
		}
		defer state.Close()
	}
	var sender *reminderSMTP
	defer func() {
		if sender != nil {
			sender.close()
		}
	}()
	if *send {
		preview, err := app.worklogReminderSnapshot(ctx, time.Now())
		if err != nil {
			return errors.New("填报状态不可用，未连接发信服务")
		}
		if preview.ShouldRemind {
			sender, err = newReminderSMTP(ctx)
			if err != nil {
				return err
			}
		}
	}
	deliver := func(ctx context.Context, recipient string, message []byte) error {
		if sender == nil {
			var err error
			sender, err = newReminderSMTP(ctx)
			if err != nil {
				return err
			}
		}
		return sender.send(ctx, recipient, message)
	}
	result, err := app.executeMailReminders(ctx, state, *send, time.Now, deliver)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		return encodeErr
	}
	return err
}

func openReminderReadOnlyDB(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA query_only=ON", "PRAGMA busy_timeout=5000"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &DB{DB: db}, nil
}

func openReminderMailState(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;
		CREATE TABLE IF NOT EXISTS reminder_mail_attempts (
			week_end TEXT NOT NULL, user_id INTEGER NOT NULL, status TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(week_end,user_id));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func validReminderMailbox(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Count(value, "@") == 1
}

func (a *App) reminderRecipient(ctx context.Context, weekEnd string, userID int64) (*User, error) {
	var user User
	err := a.db.QueryRowContext(ctx, `SELECT id,name,email FROM users u WHERE id=? AND active=1 AND is_test_user=0
		AND NOT EXISTS (SELECT 1 FROM actual_work_entries w WHERE w.user_id=u.id AND w.week_end=?)
		AND NOT EXISTS (SELECT 1 FROM leave_records l WHERE l.user_id=u.id AND l.week_end=?)`,
		userID, weekEnd, weekEnd).Scan(&user.ID, &user.Name, &user.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &user, err
}

func (a *App) executeMailReminders(ctx context.Context, state *sql.DB, send bool, clock func() time.Time, deliver reminderDeliver) (result MailReminderResult, err error) {
	result = MailReminderResult{Status: "dry_run"}
	defer func() {
		if err != nil {
			result.Status = "failed"
		}
	}()
	snapshot, err := a.worklogReminderSnapshot(ctx, clock())
	if err != nil {
		return result, errors.New("填报状态不可用，未继续发送")
	}
	result.WeekEnd, result.Missing = snapshot.WeekEnd, len(snapshot.Missing)
	if !snapshot.WindowOpen {
		result.Status = "window_closed"
		return result, nil
	}
	if !snapshot.ShouldRemind {
		result.Status = "all_submitted"
		return result, nil
	}
	from := envOr("APP_SMTP_USERNAME", "")
	baseURL, parseErr := url.Parse(a.cfg.BaseURL)
	if send && (!validReminderMailbox(from) || parseErr != nil || baseURL.Scheme != "https" || baseURL.Host == "" || baseURL.User != nil || state == nil) {
		return result, errors.New("发信配置无效，未继续发送")
	}
	for _, candidate := range snapshot.Missing {
		if err := ctx.Err(); err != nil {
			return result, errors.New("提醒任务超时，未继续发送")
		}
		// Re-read the configured window and this user's submission just before each send.
		fresh, err := a.worklogReminderSnapshot(ctx, clock())
		if err != nil {
			return result, errors.New("再次核对填报状态失败，未继续发送")
		}
		if !fresh.WindowOpen || fresh.WeekEnd != snapshot.WeekEnd {
			result.Status = "window_changed"
			return result, nil
		}
		user, err := a.reminderRecipient(ctx, snapshot.WeekEnd, candidate.UserID)
		if err != nil {
			return result, errors.New("收件人核对失败，未继续发送")
		}
		if user == nil {
			result.Skipped++
			continue
		}
		if !validReminderMailbox(user.Email) {
			result.Invalid++
			continue
		}
		if !send {
			continue
		}
		claim, err := state.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_mail_attempts(week_end,user_id,status) VALUES (?,?,'pending')`, snapshot.WeekEnd, user.ID)
		if err != nil {
			return result, errors.New("发信记录写入失败，未继续发送")
		}
		claimed, err := claim.RowsAffected()
		if err != nil {
			return result, errors.New("发信记录确认失败，未继续发送")
		}
		if claimed == 0 {
			result.Skipped++
			continue
		}
		// A successful claim precedes SMTP. A crash or ambiguous SMTP result is not retried automatically.
		message := reminderMessage(from, *user, fresh, a.cfg.BaseURL, clock().In(a.cfg.Location))
		status := "sent"
		sendErr := deliver(ctx, user.Email, message)
		if sendErr != nil {
			status = "uncertain"
			result.Uncertain++
		} else {
			result.Sent++
		}
		_, recordErr := state.ExecContext(ctx, `UPDATE reminder_mail_attempts SET status=?,updated_at=CURRENT_TIMESTAMP WHERE week_end=? AND user_id=?`, status, snapshot.WeekEnd, user.ID)
		if recordErr != nil {
			return result, errors.New("投递结果记录失败；停止且不自动重发")
		}
		if sendErr != nil {
			return result, errors.New("SMTP投递未确认；停止且不自动重发")
		}
	}
	if send {
		result.Status = "complete"
	}
	if result.Invalid > 0 {
		return result, errors.New("存在无效邮箱，相关员工未发送；请检查发信摘要")
	}
	return result, nil
}

func reminderMessage(from string, user User, snapshot *WorklogReminderResponse, baseURL string, now time.Time) []byte {
	end, _ := time.Parse(time.RFC3339, snapshot.WindowEnd)
	body := fmt.Sprintf("%s，你好：\n\n载衡系统检查到你尚未提交填报周 %s 的工时。\n请在 %s 前登录填报：\n%s/worklog\n\n即使本周没有工作，也请提交0工时；仅请假也需要提交。\n如果刚刚已完成提交，请忽略此邮件。\n\n这是系统自动提醒，仅发送给你本人。\n", user.Name, snapshot.WeekEnd,
		end.In(now.Location()).Format("2006-01-02 15:04 MST"), strings.TrimRight(baseURL, "/"))
	return encodeReminderMail(from, user.Email, "载衡｜工时填报提醒（"+snapshot.WeekEnd+"）", body, snapshot.WeekEnd, user.ID, now)
}
