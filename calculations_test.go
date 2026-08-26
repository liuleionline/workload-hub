package main

import "testing"

func TestLoadRate(t *testing.T) {
	if got := loadRate(28, 28); got != 1 {
		t.Fatalf("got %v", got)
	}
	if got := loadRate(10, 0); got != 0 {
		t.Fatalf("zero capacity got %v", got)
	}
}

func TestMakeXLSX(t *testing.T) {
	n := 12.5
	book, err := makeXLSX("测试", [][]xlsxCell{{{Text: "姓名"}, {Text: "工时"}}, {{Text: "张三"}, {Number: &n}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(book) < 1000 {
		t.Fatalf("xlsx too small: %d", len(book))
	}
}
