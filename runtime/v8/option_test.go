package v8

import "testing"

func TestOptionValidateDefaultsRuntimePoolSize(t *testing.T) {
	option := &Option{}
	option.Validate()

	if option.MinSize != 10 {
		t.Fatalf("期望默认 MinSize 为 10，实际为 %d", option.MinSize)
	}
	if option.MaxSize != 100 {
		t.Fatalf("期望默认 MaxSize 为 100，实际为 %d", option.MaxSize)
	}
}
