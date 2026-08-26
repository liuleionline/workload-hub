package main

import (
	"os"
	"testing"
)

func TestWriteXLSXCompatibilityFixture(t *testing.T) {
	path := os.Getenv("XLSX_QA_PATH")
	if path == "" {
		t.Skip("XLSX_QA_PATH is only set by the external compatibility check")
	}
	hours := 40.5
	data, err := makeXLSX("周统计/测试", [][]xlsxCell{
		{{Text: "员工"}, {Text: "统计周期"}, {Text: "工地驻场"}, {Text: "总工时"}},
		{{Text: "张三"}, {Text: "截至2026年8月14日一周"}, {Number: &hours}, {Number: &hours}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
