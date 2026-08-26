package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runBackup(db *DB, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	output := fs.String("output", "", "备份文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*output) == "" {
		*output = filepath.Join("backup", "workload-"+time.Now().Format("20060102-150405")+".db")
	}
	abs, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return err
	}
	if _, err = os.Stat(abs); err == nil {
		return fmt.Errorf("备份目标已存在：%s", abs)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err = db.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		return err
	}
	if _, err = db.Exec("VACUUM INTO ?", abs); err != nil {
		return err
	}
	fmt.Println(abs)
	return nil
}
