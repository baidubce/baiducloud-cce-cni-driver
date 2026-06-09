package main

import (
	"testing"
)

// TestSomething TestSomething 是一个测试函数，用于测试某些功能或特性。
// 参数t是指向*testing.T类型的指针，表示当前正在运行的单元测试。
// 返回值没有。
func TestSomething(t *testing.T) {
	t.Log("dummy test")
}

// TestSimpleAdd 测试函数，用于简单的加法运算
// 参数t：*testing.T类型，表示测试对象
// 返回值：无返回值
func TestSimpleAdd(t *testing.T) {
	result := SimpleAdd(2, 3)
	if result != 5 {
		t.Errorf("Expected 5, got %d", result)
	}
	t.Log("SimpleAdd test passed")
}

// TestSimpleString 测试函数，用于测试简单字符串的功能
// 参数t：*testing.T类型，表示测试对象
// 返回值：无
func TestSimpleString(t *testing.T) {
	result := SimpleString()
	expected := "hello world"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
	t.Log("SimpleString test passed")
}
