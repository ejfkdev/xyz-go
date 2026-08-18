package spec

import (
	"context"
	"strings"
	"testing"

	errs "github.com/ejfkdev/xyz-go/errors"
)

type levelArgs struct {
	Level int `json:"level" desc:"级别" validate:"min=1"`
}

type userArgs struct {
	Name  string     `json:"name" desc:"用户名" required:"true" validate:"min=2"`
	Age   int        `json:"age" desc:"年龄" default:"18"`
	Mode  string     `json:"mode" desc:"模式" enum:"fast,slow"`
	Tags  []string   `json:"tags" desc:"标签"`
	Limit *int       `json:"limit" desc:"上限"`
	Token string     `json:"-" secret:"true"`
	Sub   *levelArgs `json:"sub" desc:"子配置"`
}

type userResp struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func userHandler(_ context.Context, in *userArgs) (*userResp, error) {
	if in.Name == "missing" {
		return nil, errs.New(errs.KindNotFound, "no such user")
	}
	return &userResp{Name: in.Name, Age: in.Age}, nil
}

func newUserEntry(t *testing.T) *Entry {
	t.Helper()
	e, err := Define("user.add", userHandler).Summary("add user").Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	return e
}

func TestEntrySchema(t *testing.T) {
	s := newUserEntry(t).InputSchema
	if s.Type != "object" {
		t.Fatalf("root type = %q, want object", s.Type)
	}
	if len(s.Required) != 1 || s.Required[0] != "name" {
		t.Fatalf("required = %v, want [name]", s.Required)
	}
	if _, ok := s.Properties["name"]; !ok {
		t.Fatal("schema missing property name")
	}
	age, ok := s.Properties["age"]
	if !ok || age.Type != "integer" {
		t.Fatalf("age = %v, want integer property", age)
	}
	if d, ok := age.Default.(int); !ok || d != 18 {
		t.Fatalf("age default = %v, want 18", age.Default)
	}
	mode, ok := s.Properties["mode"]
	if !ok || len(mode.Enum) != 2 {
		t.Fatalf("mode = %v, want property with 2 enum entries", mode)
	}
	if _, ok := s.Properties["token"]; ok {
		t.Fatal("token is json:\"-\" and must not appear in schema")
	}
	limit, ok := s.Properties["limit"]
	if !ok || limit.Type != "integer" {
		t.Fatalf("limit = %v, want integer schema (pointer unwrapped)", limit)
	}
	sub, ok := s.Properties["sub"]
	if !ok || sub.Type != "object" {
		t.Fatalf("sub = %v, want object schema", sub)
	}
	if _, ok := sub.Properties["level"]; !ok {
		t.Fatal("sub.level missing from schema")
	}
}

func TestInvokeHappyPath(t *testing.T) {
	e := newUserEntry(t)
	out, err := e.Invoke(context.Background(), map[string]any{"name": "alice", "age": float64(9)})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	resp, ok := out.(*userResp)
	if !ok {
		t.Fatalf("result type = %T, want *userResp", out)
	}
	if resp.Name != "alice" || resp.Age != 9 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestInvokeStringForms(t *testing.T) {
	e := newUserEntry(t)
	out, err := e.Invoke(context.Background(), map[string]any{"name": "bob", "age": "25"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := out.(*userResp).Age; got != 25 {
		t.Fatalf("age = %d, want 25", got)
	}
}

func TestInvokeAppliesDefault(t *testing.T) {
	e := newUserEntry(t)
	out, err := e.Invoke(context.Background(), map[string]any{"name": "carol"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := out.(*userResp).Age; got != 18 {
		t.Fatalf("age = %d, want default 18", got)
	}
}

func TestInvokeMissingRequired(t *testing.T) {
	e := newUserEntry(t)
	_, err := e.Invoke(context.Background(), map[string]any{"age": 1})
	if err == nil {
		t.Fatal("want error for missing required field")
	}
	if got := errs.Classify(err); got != errs.KindInvalidInput {
		t.Fatalf("kind = %s, want invalid_input", got)
	}
}

func TestInvokeValidation(t *testing.T) {
	e := newUserEntry(t)
	_, err := e.Invoke(context.Background(), map[string]any{"name": "x"})
	if err == nil {
		t.Fatal("want error for min=2")
	}
	if got := errs.Classify(err); got != errs.KindInvalidInput {
		t.Fatalf("kind = %s, want invalid_input", got)
	}
}

func TestInvokeEnum(t *testing.T) {
	e := newUserEntry(t)
	if _, err := e.Invoke(context.Background(), map[string]any{"name": "alice", "mode": "warp"}); err == nil {
		t.Fatal("want error for enum violation")
	}
	if _, err := e.Invoke(context.Background(), map[string]any{"name": "alice", "mode": "fast"}); err != nil {
		t.Fatalf("Invoke with valid enum: %v", err)
	}
}

func TestInvokeNestedAndPtr(t *testing.T) {
	e := newUserEntry(t)
	_, err := e.Invoke(context.Background(), map[string]any{
		"name":  "alice",
		"limit": float64(3),
		"sub":   map[string]any{"level": float64(1)},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
}

func TestInvokeHandlerError(t *testing.T) {
	e := newUserEntry(t)
	_, err := e.Invoke(context.Background(), map[string]any{"name": "missing"})
	if err == nil {
		t.Fatal("want error")
	}
	if got := errs.Classify(err); got != errs.KindNotFound {
		t.Fatalf("kind = %s, want not_found", got)
	}
}

func TestUnsupportedType(t *testing.T) {
	type badArgs struct {
		M map[string]string `json:"m"`
	}
	if _, err := Define("bad.cmd", func(_ context.Context, _ *badArgs) (int, error) { return 0, nil }).Entry(); err == nil {
		t.Fatal("want error for map field")
	}
}

func TestBadName(t *testing.T) {
	if _, err := Define("bad name", userHandler).Entry(); err == nil {
		t.Fatal("want error for invalid name")
	}
}

func TestPointerTypeRejected(t *testing.T) {
	fn := func(_ context.Context, _ **userArgs) (*userResp, error) { return nil, nil }
	if _, err := Define("user.add2", fn).Entry(); err == nil {
		t.Fatal("want error for pointer type parameter")
	}
}

func TestRecursiveTypeRejected(t *testing.T) {
	type node struct {
		Next *node `json:"next"`
	}
	walk := func(_ context.Context, _ *node) (int, error) { return 0, nil }
	if _, err := Define("tree.walk", walk).Entry(); err == nil {
		t.Fatal("want error for recursive type")
	}
}

func TestEnumTemplate(t *testing.T) {
	type tmplArgs struct {
		Host string `json:"host"`
	}
	if _, err := Define("tmpl.cmd", func(_ context.Context, _ *tmplArgs) (string, error) { return "", nil }).Entry(); err != nil {
		t.Fatalf("Entry: %v", err)
	}
}

func TestInvokeSkipFieldByGoName(t *testing.T) {
	// json:"-" 字段不参与正常绑定，但通道注入（env/header）以 Go 字段名为键送达。
	type tokenArgs struct {
		Name  string `json:"name" required:"true"`
		Token string `json:"-" secret:"true"`
	}
	e, err := Define("tok.cmd", func(_ context.Context, in *tokenArgs) (string, error) {
		return "tok:" + in.Token, nil
	}).Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	out, err := e.Invoke(context.Background(), map[string]any{"name": "a", "Token": "s3cret"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out != "tok:s3cret" {
		t.Fatalf("out = %q, want token injected via Go field name", out)
	}
}

func TestInternalValidatorRules(t *testing.T) {
	type vArgs struct {
		Email string   `json:"email" validate:"omitempty,email"`
		Port  int      `json:"port" validate:"gte=1,lte=65535"`
		Mode  string   `json:"mode" validate:"required,oneof=fast slow"`
		Tags  []string `json:"tags" validate:"max=2"`
	}
	invoke := func(args map[string]any) error {
		e, err := Define("v.cmd", func(_ context.Context, in *vArgs) (string, error) { return "ok", nil }).Entry()
		if err != nil {
			t.Fatalf("Entry: %v", err)
		}
		_, err = e.Invoke(context.Background(), args)
		return err
	}
	ok := map[string]any{"mode": "fast", "port": float64(80), "email": "a@b.co", "tags": []any{"x", "y"}}
	if err := invoke(ok); err != nil {
		t.Fatalf("valid args rejected: %v", err)
	}
	// email 违规
	if err := invoke(map[string]any{"mode": "fast", "email": "nope"}); err == nil ||
		!strings.Contains(err.Error(), "email") {
		t.Fatalf("bad email not caught: %v", err)
	}
	// 数值下界
	if err := invoke(map[string]any{"mode": "fast", "port": 0}); err == nil {
		t.Fatal("gte=1 violated but passed")
	}
	// oneof
	if err := invoke(map[string]any{"mode": "warp", "port": 1}); err == nil ||
		!strings.Contains(err.Error(), "oneof") {
		t.Fatalf("oneof violated but passed: %v", err)
	}
	// slice len
	if err := invoke(map[string]any{"mode": "fast", "tags": []any{"a", "b", "c"}}); err == nil {
		t.Fatal("max=2 on slice violated but passed")
	}
	// omitempty：空邮箱跳过，其余规则照常
	if err := invoke(map[string]any{"mode": "fast", "port": 1}); err != nil {
		t.Fatalf("omitempty should skip empty email: %v", err)
	}
}

func TestUnsupportedValidateRuleRejectedAtEntry(t *testing.T) {
	type badArgs struct {
		URL string `json:"url" validate:"url"`
	}
	if _, err := Define("bad.rule", func(_ context.Context, _ *badArgs) (string, error) { return "", nil }).Entry(); err == nil {
		t.Fatal("unsupported rule url should be rejected at Entry")
	}
}

func TestOutputSchema(t *testing.T) {
	// struct 返回 → object schema（复用入参分析管线，含 json 名与 required）。
	e := newUserEntry(t)
	if e.OutputSchema == nil || e.OutputSchema.Type != "object" {
		t.Fatalf("output schema = %+v, want object", e.OutputSchema)
	}
	if _, ok := e.OutputSchema.Properties["name"]; !ok {
		t.Fatalf("output schema props = %v", e.OutputSchema.Properties)
	}
	// 切片返回 → array of object。
	type row struct {
		ID int `json:"id"`
	}
	list, err := Define("list.cmd", func(_ context.Context, _ *userArgs) ([]row, error) { return nil, nil }).Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if list.OutputSchema == nil || list.OutputSchema.Type != "array" || list.OutputSchema.Items.Type != "object" {
		t.Fatalf("list output schema = %+v", list.OutputSchema)
	}
}
