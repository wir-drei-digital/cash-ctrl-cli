package cli

import (
	"testing"
)

func TestFormEncode(t *testing.T) {
	form, err := formEncode([]byte(`{
		"name": "Anna Müller",
		"amount": 125.50,
		"count": 3,
		"big": 12345678901234567890,
		"active": true,
		"off": false,
		"skip": null,
		"items": [{"accountId": 1, "debit": 500}],
		"custom": {"a": "b"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"name":   "Anna Müller",
		"amount": "125.50",
		"count":  "3",
		"big":    "12345678901234567890",
		"active": "true",
		"off":    "false",
		"items":  `[{"accountId": 1, "debit": 500}]`,
		"custom": `{"a": "b"}`,
	}
	for k, v := range want {
		if got := form.Get(k); got != v {
			t.Errorf("form[%s] = %q, want %q", k, got, v)
		}
	}
	if form.Has("skip") {
		t.Error("null value encoded")
	}
}

func TestFormEncodeRejectsNonObject(t *testing.T) {
	for _, bad := range []string{`[1,2]`, `"x"`, `42`, `true`} {
		if _, err := formEncode([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
