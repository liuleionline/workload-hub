package main

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := hashPassword("安全Password2026")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "安全Password2026") {
		t.Fatal("正确密码未通过")
	}
	if verifyPassword(hash, "错误Password2026") {
		t.Fatal("错误密码通过验证")
	}
}
func TestPasswordMinimumLength(t *testing.T) {
	if err := validatePassword("123456"); err != nil {
		t.Fatalf("6位密码应当通过: %v", err)
	}
	if err := validatePassword("12345"); err == nil {
		t.Fatal("不足6位的密码不应通过")
	}
}

func TestPasswordMustActuallyChange(t *testing.T) {
	if err := validatePasswordChange("123456", "123456"); err == nil {
		t.Fatal("新密码与初始密码相同时不应通过")
	}
	if err := validatePasswordChange("123456", "654321"); err != nil {
		t.Fatalf("不同的6位新密码应当通过: %v", err)
	}
}

func TestTemporaryPassword(t *testing.T) {
	hash, err := hashTemporaryPassword("123456")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "123456") {
		t.Fatal("临时密码未通过")
	}
}
