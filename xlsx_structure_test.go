package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestMakeXLSXPackageStructure(t *testing.T) {
	n := 12.5
	book, err := makeXLSX("测试/工作表", [][]xlsxCell{{{Text: "姓名"}, {Text: "工时"}}, {{Text: "张三\x01"}, {Number: &n}}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
	if err != nil {
		t.Fatalf("invalid zip package: %v", err)
	}
	required := map[string]bool{
		"[Content_Types].xml": false, "_rels/.rels": false, "xl/workbook.xml": false,
		"xl/_rels/workbook.xml.rels": false, "xl/styles.xml": false, "xl/worksheets/sheet1.xml": false,
	}
	var worksheet string
	for _, file := range zr.File {
		if _, ok := required[file.Name]; ok {
			required[file.Name] = true
		}
		rc, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasSuffix(file.Name, ".xml") {
			decoder := xml.NewDecoder(bytes.NewReader(content))
			for {
				if _, decodeErr := decoder.Token(); decodeErr == io.EOF {
					break
				} else if decodeErr != nil {
					t.Fatalf("invalid XML in %s: %v", file.Name, decodeErr)
				}
			}
		}
		if file.Name == "xl/worksheets/sheet1.xml" {
			worksheet = string(content)
		}
	}
	for name, present := range required {
		if !present {
			t.Errorf("missing package part %s", name)
		}
	}
	views := strings.Index(worksheet, "<sheetViews>")
	cols := strings.Index(worksheet, "<cols>")
	data := strings.Index(worksheet, "<sheetData>")
	filter := strings.Index(worksheet, "<autoFilter")
	if views < 0 || cols < views || data < cols || filter < data {
		t.Fatalf("worksheet elements are not in OOXML schema order")
	}
}
