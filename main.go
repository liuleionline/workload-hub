package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: workload-hub <serve|init-admin|import-users|import-projects|backup>")
		os.Exit(2)
	}
	cfg := loadConfig()
	db, err := openDatabase(cfg.DatabasePath)
	if err != nil {
		slog.Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	switch os.Args[1] {
	case "init-admin":
		if err := runInitAdmin(db, os.Args[2:]); err != nil {
			slog.Error("创建管理员失败", "error", err)
			os.Exit(1)
		}
	case "import-users":
		if err := runImportUsers(db, os.Args[2:]); err != nil {
			slog.Error("导入员工失败", "error", err)
			os.Exit(1)
		}
	case "import-projects":
		if err := runImportProjects(db, os.Args[2:]); err != nil {
			slog.Error("导入项目失败", "error", err)
			os.Exit(1)
		}
	case "backup":
		if err := runBackup(db, os.Args[2:]); err != nil {
			slog.Error("备份失败", "error", err)
			os.Exit(1)
		}
	case "serve":
		if err := runServer(cfg, db); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("服务停止", "error", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "未知命令:", os.Args[1])
		os.Exit(2)
	}
}

func runInitAdmin(db *DB, args []string) error {
	fs := flag.NewFlagSet("init-admin", flag.ContinueOnError)
	email := fs.String("email", "", "管理员邮箱")
	name := fs.String("name", "系统管理员", "管理员姓名")
	password := fs.String("password", "", "临时密码；省略时交互输入")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*email) == "" {
		return errors.New("必须提供 --email")
	}
	if *password == "" {
		fmt.Print("请输入临时密码（输入会显示）：")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		*password = strings.TrimSpace(line)
	}
	if err := db.createInitialAdmin(context.Background(), strings.TrimSpace(*name), strings.TrimSpace(*email), *password); err != nil {
		return err
	}
	fmt.Println("系统管理员已创建；首次登录后必须修改密码。")
	return nil
}

func runServer(cfg Config, db *DB) error {
	app, err := NewApp(cfg, db)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	slog.Info("载衡服务已启动", "address", cfg.Addr)
	return server.ListenAndServe()
}
