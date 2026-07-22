package settings

import "testing"

func TestValidateStoredReusesRegistryValidation(t *testing.T) {
	known, err := ValidateStored(KeyScanInterval, "300")
	if err != nil || !known {
		t.Fatalf("已登记合法设置应通过校验: known=%v err=%v", known, err)
	}

	known, err = ValidateStored(KeyScanInterval, "invalid")
	if err == nil || !known || !IsValidationError(err) {
		t.Fatalf("已登记非法设置应返回 registry 校验错误: known=%v err=%v", known, err)
	}

	known, err = ValidateStored("legacy_theme", "dark")
	if err != nil || known {
		t.Fatalf("未登记设置应交由迁移兼容策略处理: known=%v err=%v", known, err)
	}
}

func TestValidateStoredValidatesRegisteredStartupPort(t *testing.T) {
	for _, value := range []string{"1", "8080", "65535"} {
		known, err := ValidateStored("server_port", value)
		if err != nil || !known {
			t.Fatalf("合法启动端口 %q 应通过校验: known=%v err=%v", value, known, err)
		}
	}

	for _, value := range []string{"", "0", "65536", "not-port"} {
		known, err := ValidateStored("server_port", value)
		if err == nil || !known || !IsValidationError(err) {
			t.Fatalf("非法启动端口 %q 应返回校验错误: known=%v err=%v", value, known, err)
		}
	}
}
