package apikey

import (
	"regexp"
	"testing"
)

// keyRe 08 §3.1 硬契约：sk-cf- + 22 位 Base62（熵≈131bit）。
var keyRe = regexp.MustCompile(`^sk-cf-[0-9A-Za-z]{22}$`)

func TestGenerateFormatAndEntropy(t *testing.T) {
	plain, prefix, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !keyRe.MatchString(plain) {
		t.Fatalf("明文格式须 sk-cf-<22 Base62>: %q", plain)
	}
	// prefix = 明文前 10 位（08 §3.1；key_prefix 列 varchar(16)）
	if prefix != plain[:10] || len(prefix) != 10 {
		t.Fatalf("prefix 应为明文前 10 位: prefix=%q plain=%q", prefix, plain)
	}
}

func TestGenerateUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		p, _, err := Generate()
		if err != nil {
			t.Fatalf("Generate[%d]: %v", i, err)
		}
		if seen[p] {
			t.Fatalf("64 次内出现重复明文: %q", p)
		}
		seen[p] = true
	}
}
