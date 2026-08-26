package main

import (
	"archive/zip"
	"bytes"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// makeXLSX builds a standards-compliant, dependency-free OOXML workbook.
// The element order in worksheet XML is significant; Excel rejects packages
// whose sheetViews/cols/sheetData elements are emitted out of schema order.
func makeXLSX(sheetName string, rows [][]xlsxCell) ([]byte, error) {
	name := sanitizeSheetName(sheetName)
	columnCount := 1
	for _, row := range rows {
		if len(row) > columnCount {
			columnCount = len(row)
		}
	}
	rowCount := len(rows)
	if rowCount < 1 {
		rowCount = 1
	}
	lastColumn := columnName(columnCount)

	var sheet bytes.Buffer
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	sheet.WriteString(`<dimension ref="A1:` + lastColumn + strconv.Itoa(rowCount) + `"/>`)
	sheet.WriteString(`<sheetViews><sheetView tabSelected="1" workbookViewId="0"><pane xSplit="2" ySplit="1" topLeftCell="C2" activePane="bottomRight" state="frozen"/><selection pane="bottomRight" activeCell="C2" sqref="C2"/></sheetView></sheetViews>`)
	sheet.WriteString(`<sheetFormatPr defaultRowHeight="15"/>`)
	sheet.WriteString(`<cols><col min="1" max="2" width="18" customWidth="1"/><col min="3" max="` + strconv.Itoa(columnCount) + `" width="16" customWidth="1"/></cols>`)
	sheet.WriteString(`<sheetData>`)
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
				sheet.WriteString(`<c r="` + ref + `" t="inlineStr"` + style + `><is><t xml:space="preserve">` + xlsxText(cell.Text) + `</t></is></c>`)
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData>`)
	if len(rows) > 0 {
		sheet.WriteString(`<autoFilter ref="A1:` + lastColumn + `1"/>`)
	}
	sheet.WriteString(`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>`)
	sheet.WriteString(`</worksheet>`)

	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`,
		"docProps/app.xml":           `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>载衡人力负荷管理</Application></Properties>`,
		"docProps/core.xml":          `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:creator>载衡人力负荷管理</dc:creator><cp:lastModifiedBy>载衡人力负荷管理</cp:lastModifiedBy></cp:coreProperties>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><bookViews><workbookView xWindow="0" yWindow="0" windowWidth="24000" windowHeight="12000"/></bookViews><sheets><sheet name="` + xlsxText(name) + `" sheetId="1" r:id="rId1"/></sheets><calcPr calcId="191029"/></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml":              `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Aptos"/><family val="2"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Aptos"/><family val="2"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF0B6655"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles><dxfs count="0"/><tableStyles count="0" defaultTableStyle="TableStyleMedium2" defaultPivotStyle="PivotStyleLight16"/></styleSheet>`,
		"xl/worksheets/sheet1.xml":   sheet.String(),
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		entry, err := zw.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write([]byte(files[path])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func sanitizeSheetName(value string) string {
	value = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\\`, r) || r < 0x20 || r == utf8.RuneError {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if value == "" {
		value = "数据"
	}
	runes := []rune(value)
	if len(runes) > 31 {
		value = string(runes[:31])
	}
	return value
}

func xlsxText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
			return r
		}
		return -1
	}, strings.ToValidUTF8(value, "�"))
	return xmlEscape(value)
}
