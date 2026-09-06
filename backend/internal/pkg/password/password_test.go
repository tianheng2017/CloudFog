package password

import (
	"errors"
	"strings"
	"testing"
)

func TestHashVerify(t *testing.T) {
	h, err := Hash("S3cret!2026")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("PHC 格式异常: %s", h)
	}
	algo, err := Verify("S3cret!2026", h)
	if err != nil || algo != "argon2id" {
		t.Fatalf("Verify 正确密码: algo=%q err=%v", algo, err)
	}
	if _, err := Verify("wrong", h); !errors.Is(err, ErrMismatch) {
		t.Fatalf("错误密码应 ErrMismatch, got %v", err)
	}
}

func TestVerifyRejectsLegacyDirect(t *testing.T) {
	// md5_legacy 存量禁止直连比对（13-operations §5.3 迁移工具职责）
	_, err := Verify("x", strings.Repeat("a", 32))
	if err == nil {
		t.Fatal("md5_legacy 直连比对应被拒绝")
	}
}

// DoS 防护：巨额参数（m=10GB/t=9999）的恶意哈希必须在分配前被拒绝，不得卡死/耗尽。
func TestVerifyRejectsAbsurdArgon2Params(t *testing.T) {
	// $argon2id$v=19$m=10485760,t=9999,p=4$<盐16字节b64>$<hash>
	poisoned := "$argon2id$v=19$m=10485760,t=9999,p=4$" +
		"c2FsdHNhbHRzYWx0c2FsdHNhbHQ=" + "$" +
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if _, err := Verify("x", poisoned); err == nil {
		t.Fatal("巨额参数应被拒绝")
	}
}
