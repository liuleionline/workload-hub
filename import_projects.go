package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type importedProject struct {
	Code            string
	Name            string
	ShortName       string
	Size            string
	ChiefDesigner   string
	Lead1           string
	Lead2           string
	ExecutingLead   string
	Creator         string
	StartDate       string
	ExpectedEndDate string
}

func runImportProjects(db *DB, args []string) error {
	fs := flag.NewFlagSet("import-projects", flag.ContinueOnError)
	filePath := fs.String("file", "", "UTF-8 CSV项目名单")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*filePath) == "" {
		return errors.New("必须提供 --file")
	}
	projects, err := readImportedProjects(*filePath)
	if err != nil {
		return err
	}
	if err := db.importProjects(context.Background(), projects); err != nil {
		return err
	}
	fmt.Printf("已导入%d个项目；项目编号、全名或开始日期为空的项目将显示为待完善，并提醒对应执行专业负责人校核。\n", len(projects))
	return nil
}

func readImportedProjects(path string) ([]importedProject, error) {
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
	codeColumn := column("项目编号", "code")
	nameColumn := column("项目名称", "name")
	shortNameColumn := column("项目简称", "short_name")
	sizeColumn := column("项目类型", "size")
	chiefColumn := column("项目负责人（总设计师）", "项目负责人/总设计师", "chief_designer")
	lead1Column := column("专业负责人1", "lead1")
	lead2Column := column("专业负责人2", "lead2")
	executingColumn := column("执行专业负责人", "executing_lead")
	creatorColumn := column("创建人", "creator")
	startColumn := column("开始日期", "start_date")
	endColumn := column("预计结束日期", "expected_end_date")
	if shortNameColumn < 0 || sizeColumn < 0 || chiefColumn < 0 || lead1Column < 0 || executingColumn < 0 || creatorColumn < 0 {
		return nil, errors.New("CSV缺少项目简称、项目类型、总设计师、专业负责人、执行负责人或创建人列")
	}
	valueAt := func(record []string, index int) string {
		if index < 0 || index >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[index])
	}
	var projects []importedProject
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
		project := importedProject{
			Code:            valueAt(record, codeColumn),
			Name:            valueAt(record, nameColumn),
			ShortName:       valueAt(record, shortNameColumn),
			Size:            valueAt(record, sizeColumn),
			ChiefDesigner:   valueAt(record, chiefColumn),
			Lead1:           valueAt(record, lead1Column),
			Lead2:           valueAt(record, lead2Column),
			ExecutingLead:   valueAt(record, executingColumn),
			Creator:         valueAt(record, creatorColumn),
			StartDate:       valueAt(record, startColumn),
			ExpectedEndDate: valueAt(record, endColumn),
		}
		if project.ShortName == "" || project.ChiefDesigner == "" || project.Lead1 == "" || project.ExecutingLead == "" || project.Creator == "" {
			return nil, fmt.Errorf("第%d行缺少项目简称、总设计师、专业负责人、执行负责人或创建人", row)
		}
		if project.Size != "超大" && project.Size != "大" && project.Size != "中" && project.Size != "小" {
			return nil, fmt.Errorf("第%d行项目类型无效: %s", row, project.Size)
		}
		if project.Size != "超大" && project.Lead2 != "" {
			return nil, fmt.Errorf("第%d行只有超大项目可以填写第二位专业负责人", row)
		}
		for label, dateValue := range map[string]string{"开始日期": project.StartDate, "预计结束日期": project.ExpectedEndDate} {
			if dateValue != "" {
				if _, err := time.Parse("2006-01-02", dateValue); err != nil {
					return nil, fmt.Errorf("第%d行%s格式应为yyyy-mm-dd", row, label)
				}
			}
		}
		projects = append(projects, project)
	}
	if len(projects) == 0 {
		return nil, errors.New("CSV中没有项目数据")
	}
	return projects, nil
}

func (db *DB) importProjects(ctx context.Context, projects []importedProject) error {
	var existingCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		return errors.New("系统中已有项目；为避免重复导入，批量初始化已停止")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for index, project := range projects {
		code := project.Code
		if code == "" {
			code = fmt.Sprintf("INIT-%03d", index+1)
		}
		leadIDs := []int64{}
		lead1ID, err := importedProjectUserID(ctx, tx, project.Lead1, true)
		if err != nil {
			return fmt.Errorf("项目%s的专业负责人1无效: %w", project.ShortName, err)
		}
		leadIDs = append(leadIDs, lead1ID)
		if project.Lead2 != "" {
			lead2ID, err := importedProjectUserID(ctx, tx, project.Lead2, true)
			if err != nil {
				return fmt.Errorf("项目%s的专业负责人2无效: %w", project.ShortName, err)
			}
			if lead2ID == lead1ID {
				return fmt.Errorf("项目%s的两位专业负责人不能相同", project.ShortName)
			}
			leadIDs = append(leadIDs, lead2ID)
		}
		executingID, err := importedProjectUserID(ctx, tx, project.ExecutingLead, true)
		if err != nil {
			return fmt.Errorf("项目%s的执行专业负责人无效: %w", project.ShortName, err)
		}
		creatorID, err := importedProjectUserID(ctx, tx, project.Creator, false)
		if err != nil {
			return fmt.Errorf("项目%s的创建人无效: %w", project.ShortName, err)
		}
		isExecutionLead := false
		for _, leadID := range leadIDs {
			if leadID == executingID {
				isExecutionLead = true
			}
		}
		if !isExecutionLead {
			return fmt.Errorf("项目%s的执行专业负责人必须是专业负责人之一", project.ShortName)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date)
			VALUES (?,?,?,?,?,?,?,?,?)`, code, project.Name, project.ShortName, project.Size, project.ChiefDesigner, creatorID, executingID, project.StartDate, project.ExpectedEndDate)
		if err != nil {
			return fmt.Errorf("导入项目%s失败: %w", project.ShortName, err)
		}
		projectID, _ := result.LastInsertId()
		for _, leadID := range leadIDs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO project_leads(project_id,user_id,is_execution) VALUES (?,?,?)", projectID, leadID, leadID == executingID); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(action,entity_type,entity_id,detail) VALUES ('bulk_project_import','project','',?)", fmt.Sprintf("初始批量导入%d个项目", len(projects)))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func importedProjectUserID(ctx context.Context, tx *sql.Tx, name string, requireLead bool) (int64, error) {
	var id int64
	var role string
	var active bool
	if err := tx.QueryRowContext(ctx, "SELECT id,role,active FROM users WHERE name=?", strings.TrimSpace(name)).Scan(&id, &role, &active); err != nil {
		return 0, errors.New("员工姓名不存在")
	}
	if !active {
		return 0, errors.New("员工账号未启用")
	}
	if requireLead && role != "lead" && role != "manager" {
		return 0, errors.New("必须是部门领导或专业负责人")
	}
	return id, nil
}
