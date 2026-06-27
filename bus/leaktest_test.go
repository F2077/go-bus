package bus

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain 为本包所有测试装设协程泄漏之哨：任一 goroutine 寿逾其测试者
// （如未 Cancel 之 listener），测试即败。此乃 goleak 标准 TestMain 模式：
// https://pkg.go.dev/go.uber.org/goleak#VerifyTestMain
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
