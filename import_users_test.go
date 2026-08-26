package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportUsers(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "users.csv")
	csvData := "姓名,层级,是否系统管理员,移动电话,企业邮箱,执业资格,职称,身份证号码后6位\n" +
		"测试管理员,管理者,是,13000000000,admin@example.com,一级注册结构工程师,高级工程师,012345\n" +
		"测试员工,一般设计人员,否,13100000000,user@example.com,,工程师,123456\n"
	if err := os.WriteFile(csvPath, []byte(csvData), 0o600); err != nil {
		t.Fatal(err)
	}
	users, err := readImportedUsers(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(filepath.Join(dir, "workload.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.importUsers(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	var count, admins, mustChange int
	if err := db.QueryRow("SELECT COUNT(*),SUM(is_system_admin),SUM(must_change_password) FROM users").Scan(&count, &admins, &mustChange); err != nil {
		t.Fatal(err)
	}
	if count != 2 || admins != 1 || mustChange != 2 {
		t.Fatalf("unexpected import totals: count=%d admins=%d mustChange=%d", count, admins, mustChange)
	}
	var mobile, qualification, title, passwordHash string
	if err := db.QueryRow("SELECT mobile,qualification,professional_title,password_hash FROM users WHERE email='admin@example.com'").Scan(&mobile, &qualification, &title, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if mobile != "13000000000" || qualification == "" || title != "高级工程师" || !verifyPassword(passwordHash, "012345") {
		t.Fatal("imported employee profile or password hash is incorrect")
	}
	if err := db.importUsers(context.Background(), users); err == nil {
		t.Fatal("re-import into a non-empty database should fail")
	}
}
