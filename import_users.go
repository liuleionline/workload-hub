package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type importedUser struct {
	Name              string
	Role              string
	IsSystemAdmin     bool
	Mobile            string
	Email             string
	Qualification     string
	ProfessionalTitle string
	TemporaryPassword string
}

func runImportUsers(db *DB, args []string) error {
	fs := flag.NewFlagSet("import-users", flag.ContinueOnError)
	filePath := fs.String("file", "", "UTF-8 CSV员工名单")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*filePath) == "" {
		return errors.New("必须提供 --file")
	}
	users, err := readImportedUsers(*filePath)
	if err != nil {
		return err
	}
	if err := db.importUsers(context.Background(), users); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, user := range users {
		counts[user.Role]++
	}
	fmt.Printf("已导入%d名员工：部门领导%d、专业负责人%d、设计师%d；首次登录均须修改密码。\n", len(users), counts["manager"], counts["lead"], counts["designer"])
	return nil
}

func readImportedUsers(path string) ([]importedUser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取CSV表头: %w", err)
	}
	indexes := map[string]int{}
	for index, value := range header {
		indexes[strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))] = index
	}
	column := func(names ...string) int {
		for _, name := range names {
			if index, ok := indexes[name]; ok {
				return index
			}
		}
		return -1
	}
	nameColumn := column("姓名", "name")
	roleColumn := column("层级", "role")
	adminColumn := column("是否系统管理员", "is_system_admin")
	mobileColumn := column("移动电话", "mobile")
	emailColumn := column("企业邮箱", "email")
	qualificationColumn := column("执业资格", "qualification")
	titleColumn := column("职称", "professional_title")
	passwordColumn := column("身份证号码后6位", "初始密码", "temporary_password")
	if nameColumn < 0 || roleColumn < 0 || emailColumn < 0 || passwordColumn < 0 {
		return nil, errors.New("CSV缺少姓名、层级、企业邮箱或初始密码列")
	}
	valueAt := func(record []string, index int) string {
		if index < 0 || index >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[index])
	}
	var users []importedUser
	seenEmails := map[string]bool{}
	for row := 2; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取CSV第%d行: %w", row, err)
		}
		if strings.TrimSpace(strings.Join(record, "")) == "" {
			continue
		}
		role, err := importedRole(valueAt(record, roleColumn))
		if err != nil {
			return nil, fmt.Errorf("第%d行: %w", row, err)
		}
		email := strings.ToLower(valueAt(record, emailColumn))
		user := importedUser{
			Name:              valueAt(record, nameColumn),
			Role:              role,
			IsSystemAdmin:     importedBool(valueAt(record, adminColumn)),
			Mobile:            valueAt(record, mobileColumn),
			Email:             email,
			Qualification:     valueAt(record, qualificationColumn),
			ProfessionalTitle: valueAt(record, titleColumn),
			TemporaryPassword: valueAt(record, passwordColumn),
		}
		if user.Name == "" || email == "" || !strings.Contains(email, "@") {
			return nil, fmt.Errorf("第%d行姓名或邮箱无效", row)
		}
		if seenEmails[email] {
			return nil, fmt.Errorf("第%d行邮箱重复: %s", row, email)
		}
		if len([]rune(user.TemporaryPassword)) < 6 {
			return nil, fmt.Errorf("第%d行临时密码至少需要6个字符", row)
		}
		seenEmails[email] = true
		users = append(users, user)
	}
	if len(users) == 0 {
		return nil, errors.New("CSV中没有员工数据")
	}
	return users, nil
}

func importedRole(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "部门领导", "管理者", "manager":
		return "manager", nil
	case "专业负责人", "lead":
		return "lead", nil
	case "设计师", "一般设计人员", "designer":
		return "designer", nil
	default:
		return "", fmt.Errorf("未知员工层级: %s", value)
	}
}

func importedBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "是", "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func (db *DB) importUsers(ctx context.Context, users []importedUser) error {
	var existingCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		return errors.New("系统中已有员工；为避免覆盖账号，批量初始化已停止")
	}
	hasSystemAdmin := false
	for _, user := range users {
		hasSystemAdmin = hasSystemAdmin || user.IsSystemAdmin
	}
	if !hasSystemAdmin {
		return errors.New("初始名单必须至少包含一名系统管理员")
	}
	var departmentID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM departments WHERE active=1 ORDER BY id LIMIT 1").Scan(&departmentID); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, user := range users {
		hash, err := hashTemporaryPassword(user.TemporaryPassword)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO users(department_id,name,email,mobile,qualification,professional_title,password_hash,role,is_system_admin,must_change_password)
			VALUES (?,?,?,?,?,?,?,?,?,1)`, departmentID, user.Name, user.Email, user.Mobile, user.Qualification, user.ProfessionalTitle, hash, user.Role, user.IsSystemAdmin)
		if err != nil {
			return fmt.Errorf("导入%s失败: %w", user.Email, err)
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(action,entity_type,entity_id,detail) VALUES ('bulk_user_import','user','',?)", fmt.Sprintf("初始批量导入%d名员工", len(users)))
	if err != nil {
		return err
	}
	return tx.Commit()
}
