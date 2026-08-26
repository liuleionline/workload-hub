package main

import (
	"net/http"
	"net/url"
	"strconv"
)

type summaryUser struct {
	ID   int64
	Name string
	Role string
}

type summaryProject struct {
	ID    int64
	Label string
}

func (a *App) handleSummaryExcelExport(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil || !user.IsSystemAdmin {
		http.Error(w, "只有系统管理员可以下载全部人员、全部项目的总表", http.StatusForbidden)
		return
	}
	period, err := a.reportData(r)
	if err != nil {
		http.Error(w, "导出数据读取失败", http.StatusInternalServerError)
		return
	}
	userRows, err := a.db.QueryContext(r.Context(), "SELECT id,name,role FROM users WHERE is_test_user=0 ORDER BY active DESC,name")
	if err != nil {
		http.Error(w, "员工数据读取失败", http.StatusInternalServerError)
		return
	}
	users := []summaryUser{}
	for userRows.Next() {
		var item summaryUser
		if err = userRows.Scan(&item.ID, &item.Name, &item.Role); err != nil {
			userRows.Close()
			http.Error(w, "员工数据读取失败", http.StatusInternalServerError)
			return
		}
		users = append(users, item)
	}
	userRows.Close()
	projectRows, err := a.db.QueryContext(r.Context(), "SELECT p.id,p.short_name,p.code FROM projects p JOIN users pc ON pc.id=p.creator_user_id WHERE pc.is_test_user=0 ORDER BY p.short_name,p.code")
	if err != nil {
		http.Error(w, "项目数据读取失败", http.StatusInternalServerError)
		return
	}
	projects := []summaryProject{}
	for projectRows.Next() {
		var item summaryProject
		var shortName, code string
		if err = projectRows.Scan(&item.ID, &shortName, &code); err != nil {
			projectRows.Close()
			http.Error(w, "项目数据读取失败", http.StatusInternalServerError)
			return
		}
		item.Label = shortName + "（" + code + "）"
		projects = append(projects, item)
	}
	projectRows.Close()
	values := map[int64]map[int64]float64{}
	others := map[int64]float64{}
	sites := map[int64]float64{}
	rows, err := a.db.QueryContext(r.Context(), `SELECT a.user_id,COALESCE(a.project_id,0),SUM(a.hours),SUM(CASE WHEN a.work_category='site' THEN a.hours ELSE 0 END)
		FROM actual_work_entries a
		JOIN users u ON u.id=a.user_id
		LEFT JOIN projects p ON p.id=a.project_id
		LEFT JOIN users pc ON pc.id=p.creator_user_id
		WHERE a.week_end>=? AND a.week_end<? AND u.is_test_user=0
			AND (a.project_id IS NULL OR pc.is_test_user=0)
		GROUP BY a.user_id,a.project_id`, period.StartDate, period.EndDate)
	if err != nil {
		http.Error(w, "工时数据读取失败", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var userID, projectID int64
		var hours, siteHours float64
		if err = rows.Scan(&userID, &projectID, &hours, &siteHours); err != nil {
			rows.Close()
			http.Error(w, "工时数据读取失败", http.StatusInternalServerError)
			return
		}
		sites[userID] += siteHours
		if projectID == 0 {
			others[userID] += hours
			continue
		}
		if values[userID] == nil {
			values[userID] = map[int64]float64{}
		}
		values[userID][projectID] += hours
	}
	rows.Close()
	headers := []string{"员工", "人员类型"}
	for _, project := range projects {
		headers = append(headers, project.Label)
	}
	headers = append(headers, "工地驻场", "其它工作", "个人合计")
	xlsxRows := [][]xlsxCell{makeTextCells(headers)}
	projectTotals := map[int64]float64{}
	otherTotal := 0.0
	siteTotal := 0.0
	grandTotal := 0.0
	for _, item := range users {
		row := []xlsxCell{{Text: item.Name}, {Text: (User{Role: item.Role}).RoleName()}}
		total := 0.0
		for _, project := range projects {
			hours := values[item.ID][project.ID]
			value := hours
			row = append(row, xlsxCell{Number: &value})
			total += hours
			projectTotals[project.ID] += hours
		}
		site := sites[item.ID]
		other := others[item.ID]
		siteTotal += site
		total += other
		otherTotal += other
		grandTotal += total
		siteValue, otherValue, totalValue := site, other, total
		row = append(row, xlsxCell{Number: &siteValue}, xlsxCell{Number: &otherValue}, xlsxCell{Number: &totalValue})
		xlsxRows = append(xlsxRows, row)
	}
	totalRow := []xlsxCell{{Text: "项目合计"}, {Text: ""}}
	for _, project := range projects {
		value := projectTotals[project.ID]
		totalRow = append(totalRow, xlsxCell{Number: &value})
	}
	siteValue, otherValue, grandValue := siteTotal, otherTotal, grandTotal
	totalRow = append(totalRow, xlsxCell{Number: &siteValue}, xlsxCell{Number: &otherValue}, xlsxCell{Number: &grandValue})
	xlsxRows = append(xlsxRows, totalRow)
	book, err := makeXLSX("员工项目工时总表", xlsxRows)
	if err != nil {
		http.Error(w, "生成Excel失败", http.StatusInternalServerError)
		return
	}
	filename := "载衡_" + period.PeriodLabel + "_员工项目工时总表.xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(book)))
	_, _ = w.Write(book)
	a.audit(r.Context(), &user.ID, "export_summary_xlsx", "report", strconv.Itoa(period.Year), filename, clientIP(r))
}

func makeTextCells(values []string) []xlsxCell {
	result := make([]xlsxCell, len(values))
	for i, value := range values {
		result[i] = xlsxCell{Text: value}
	}
	return result
}
