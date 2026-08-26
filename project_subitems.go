package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func parseProjectSubitems(r *http.Request) ([]ProjectSubitem, error) {
	ids := r.Form["subitem_id[]"]
	names := r.Form["subitem_name[]"]
	areas := r.Form["subitem_area[]"]
	structures := r.Form["subitem_structure[]"]
	notes := r.Form["subitem_notes[]"]
	subitems := make([]ProjectSubitem, 0, len(names))
	for i := 0; i < len(names); i++ {
		name := strings.TrimSpace(valueAt(names, i))
		areaText := strings.TrimSpace(valueAt(areas, i))
		structure := strings.TrimSpace(valueAt(structures, i))
		note := strings.TrimSpace(valueAt(notes, i))
		rawID := strings.TrimSpace(valueAt(ids, i))
		if name == "" && areaText == "" && structure == "" && note == "" {
			continue
		}
		if name == "" {
			return subitems, fmt.Errorf("已添加的子项必须填写子项号及子项名称")
		}
		var area float64
		if areaText != "" {
			var err error
			area, err = strconv.ParseFloat(areaText, 64)
			if err != nil || area < 0 {
				return subitems, fmt.Errorf("子项建筑面积必须是大于或等于0的数字")
			}
		}
		var id int64
		if rawID != "" {
			id, _ = parseID(rawID)
		}
		subitems = append(subitems, ProjectSubitem{ID: id, Name: name, Area: area, Structure: structure, Notes: note, Active: true})
	}
	return subitems, nil
}

func (a *App) projectSubitems(ctx context.Context, projectID int64, activeOnly bool) ([]ProjectSubitem, error) {
	query := "SELECT id,project_id,name,area,structure,notes,active FROM project_subitems WHERE project_id=?"
	if activeOnly {
		query += " AND active=1"
	}
	query += " ORDER BY sort_order,id"
	rows, err := a.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProjectSubitem{}
	for rows.Next() {
		var item ProjectSubitem
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Name, &item.Area, &item.Structure, &item.Notes, &item.Active); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *App) saveProjectSubitems(ctx context.Context, tx *sql.Tx, projectID int64, items []ProjectSubitem) error {
	kept := map[int64]bool{}
	for index, item := range items {
		if item.ID > 0 {
			result, err := tx.ExecContext(ctx, "UPDATE project_subitems SET name=?,area=?,structure=?,notes=?,active=1,sort_order=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND project_id=?",
				item.Name, item.Area, item.Structure, item.Notes, index, item.ID, projectID)
			if err != nil {
				return err
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				return fmt.Errorf("子项信息无效，请刷新页面后重试")
			}
			kept[item.ID] = true
			continue
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO project_subitems(project_id,name,area,structure,notes,active,sort_order) VALUES (?,?,?,?,?,1,?)",
			projectID, item.Name, item.Area, item.Structure, item.Notes, index)
		if err != nil {
			return err
		}
		id, _ := result.LastInsertId()
		kept[id] = true
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM project_subitems WHERE project_id=? AND active=1", projectID)
	if err != nil {
		return err
	}
	var deactivate []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !kept[id] {
			deactivate = append(deactivate, id)
		}
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, id := range deactivate {
		if _, err = tx.ExecContext(ctx, "UPDATE project_subitems SET active=0,updated_at=CURRENT_TIMESTAMP WHERE id=?", id); err != nil {
			return err
		}
	}
	return nil
}

func nullableID(id int64) any {
	if id > 0 {
		return id
	}
	return nil
}

func (a *App) activeProjectSubitemMap(ctx context.Context, projects []Project) (map[int64][]ProjectSubitem, error) {
	result := make(map[int64][]ProjectSubitem, len(projects))
	for _, project := range projects {
		items, err := a.projectSubitems(ctx, project.ID, true)
		if err != nil {
			return nil, err
		}
		result[project.ID] = items
	}
	return result, nil
}
