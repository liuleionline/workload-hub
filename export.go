package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"net/http"
	"net/url"
	"strconv"
)

func (a *App) handleExcelExport(w http.ResponseWriter, r *http.Request) {
	data, err := a.reportData(r)
	if err != nil {
		http.Error(w, "导出数据读取失败", 500)
		return
	}
	headers := []string{"周次", "员工", "项目编号", "项目简称", "项目名称", "工作类别", "承担工作/其它内容", "实际工时", "个人完成"}
	rows := make([][]xlsxCell, 0, len(data.Rows)+1)
	header := make([]xlsxCell, len(headers))
	for i, v := range headers {
		header[i] = xlsxCell{Text: v}
	}
	rows = append(rows, header)
	for _, item := range data.Rows {
		content := item.WorkContent
		if item.OtherDescription != "" {
			content = item.OtherDescription
		}
		completed := "否"
		category := "常规项目工作"
		if item.ProjectCode == "其它" {
			category = "其它工作"
		} else if item.WorkCategory == "site" {
			category = "工地驻场"
		}
		if item.Completed {
			completed = "是"
		}
		rows = append(rows, []xlsxCell{{Text: item.WeekEnd}, {Text: item.UserName}, {Text: item.ProjectCode}, {Text: item.ProjectShortName}, {Text: item.ProjectName}, {Text: category}, {Text: content}, {Number: &item.Hours}, {Text: completed}})
	}
	book, err := makeXLSX("工时明细", rows)
	if err != nil {
		http.Error(w, "生成Excel失败", 500)
		return
	}
	filename := "载衡_" + data.PeriodLabel + "_工时明细.xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(book)))
	_, _ = w.Write(book)
	user := currentUser(r)
	a.audit(r.Context(), &user.ID, "export_xlsx", "report", strconv.Itoa(data.Year), filename, clientIP(r))
}

type xlsxCell struct {
	Text   string
	Number *float64
}

func makeLegacyXLSX(sheetName string, rows [][]xlsxCell) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml":              `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Aptos"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Aptos"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF0B6655"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs></styleSheet>`,
	}
	name := xmlEscape(sheetName)
	files["xl/workbook.xml"] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + name + `" sheetId="1" r:id="rId1"/></sheets></workbook>`
	for path, content := range files {
		entry, err := zw.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	entry, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	var sheet bytes.Buffer
	columnCount := 1
	if len(rows) > 0 && len(rows[0]) > columnCount {
		columnCount = len(rows[0])
	}
	lastColumn := columnName(columnCount)
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols><col min="1" max="2" width="18" customWidth="1"/><col min="3" max="` + strconv.Itoa(columnCount) + `" width="16" customWidth="1"/></cols><sheetData>`)
	for rowIndex, row := range rows {
		sheet.WriteString(`<row r="` + strconv.Itoa(rowIndex+1) + `">`)
		for colIndex, cell := range row {
			ref := columnName(colIndex+1) + strconv.Itoa(rowIndex+1)
			style := ""
			if rowIndex == 0 {
				style = ` s="1"`
			}
			if cell.Number != nil {
				sheet.WriteString(`<c r="` + ref + `"` + style + `><v>` + strconv.FormatFloat(*cell.Number, 'f', 2, 64) + `</v></c>`)
			} else {
				sheet.WriteString(`<c r="` + ref + `" t="inlineStr"` + style + `><is><t>` + xmlEscape(cell.Text) + `</t></is></c>`)
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData><autoFilter ref="A1:` + lastColumn + `1"/><sheetViews><sheetView workbookViewId="0"><pane xSplit="2" ySplit="1" topLeftCell="C2" activePane="bottomRight" state="frozen"/></sheetView></sheetViews></worksheet>`)
	if _, err = entry.Write(sheet.Bytes()); err != nil {
		return nil, err
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func xmlEscape(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}
func columnName(n int) string {
	result := ""
	for n > 0 {
		n--
		result = string(rune('A'+n%26)) + result
		n /= 26
	}
	return result
}
