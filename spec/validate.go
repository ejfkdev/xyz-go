package spec

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	errs "github.com/ejfkdev/xyz-go/errors"
)

// 库内 validator：实现 go-playground/validator 语法的兼容子集，零第三方依赖。
// 不支持的规则在注册期（Entry 构建）报错——与「注册期即报错」原则一致，
// 而不是运行时悄悄忽略。

// vrule 是一条已解析的校验规则；num 是数值参数的解析结果（-1 表示非数值规则）。
type vrule struct {
	key  string
	args []string
	num  float64
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// numericRuleKeys 是带一个数值参数的规则集合。
var numericRuleKeys = map[string]bool{
	"min": true, "max": true, "len": true,
	"gt": true, "gte": true, "lt": true, "lte": true,
}

// supportedRuleKeys 是本实现支持的规则全集。
var supportedRuleKeys = map[string]bool{
	"required": true, "omitempty": true,
	"min": true, "max": true, "len": true,
	"gt": true, "gte": true, "lt": true, "lte": true,
	"oneof": true, "email": true,
}

// parseValidateTag 解析 validate 标签。规则用逗号分隔，参数用 = 或空格给出
// （与 go-playground/validator 一致，"required,min=2,oneof=fast slow"）。
func parseValidateTag(v string) ([]vrule, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var out []vrule
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, rest, _ := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		if !supportedRuleKeys[key] {
			return nil, fmt.Errorf("unsupported validate rule %q (supported: required, omitempty, min, max, len, gt, gte, lt, lte, oneof, email)", key)
		}
		rule := vrule{key: key, num: -1, args: strings.Fields(rest)}
		if numericRuleKeys[key] {
			if len(rule.args) != 1 {
				return nil, fmt.Errorf("validate rule %q needs exactly one numeric argument, got %q", key, rest)
			}
			n, err := strconv.ParseFloat(rule.args[0], 64)
			if err != nil {
				return nil, fmt.Errorf("validate rule %q: %q is not a number", key, rule.args[0])
			}
			rule.num = n
		}
		out = append(out, rule)
	}
	return out, nil
}

// runValidation 递归执行字段树上的规则。首条失败即返回（形状与旧行为一致：
// invalid_input 分类 + "invalid value for field %q: %s" 消息）。
func runValidation(in any, root *FieldMeta) error {
	return validateNode(reflect.ValueOf(in), root)
}

func validateNode(rv reflect.Value, node *FieldMeta) error {
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct || node == nil {
		return nil
	}
	for _, f := range node.Fields {
		fval := rv.FieldByIndex(f.Index)
		if err := checkRules(f, fval); err != nil {
			return err
		}
		// 递归嵌套：值 struct、指针-to-struct、slice-of-struct（时间等叶子除外）。
		switch {
		case f.Kind == reflect.Struct && f.Fields != nil && f.Type != timeType:
			if err := validateNode(fval, f); err != nil {
				return err
			}
		case f.Kind == reflect.Ptr && f.Elem != nil && f.Elem.Kind == reflect.Struct && f.Elem.Type != timeType:
			if !fval.IsNil() {
				if err := validateNode(fval, f); err != nil {
					return err
				}
			}
		case f.Kind == reflect.Slice && f.Elem != nil && f.Elem.Kind == reflect.Struct && f.Elem.Type != timeType:
			for i := 0; i < fval.Len(); i++ {
				if err := validateNode(fval.Index(i), f.Elem); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkRules(f *FieldMeta, fv reflect.Value) error {
	if len(f.rules) == 0 {
		return nil
	}
	zero := isZero(fv)
	for _, r := range f.rules {
		if r.key == "omitempty" {
			if zero {
				break // 空值：跳过本字段全部校验
			}
			continue
		}
		if r.key == "required" {
			if zero {
				return fieldRuleError(f, "required")
			}
			continue
		}
		if !ruleOK(r, fv) {
			return fieldRuleError(f, r.key)
		}
	}
	return nil
}

func fieldRuleError(f *FieldMeta, rule string) error {
	name := f.JSONName
	if name == "" {
		name = f.Name
	}
	return errs.Errorf(errs.KindInvalidInput, "invalid value for field %q: %s", name, rule)
}

func ruleOK(r vrule, v reflect.Value) bool {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	switch r.key {
	case "min", "max", "len":
		var n float64
		switch v.Kind() {
		case reflect.String, reflect.Slice, reflect.Array:
			n = float64(v.Len())
		default:
			n = numericOf(v)
		}
		switch r.key {
		case "min":
			return n >= r.num
		case "max":
			return n <= r.num
		default:
			return n == r.num
		}
	case "gt", "gte", "lt", "lte":
		if !isNumericKind(v.Kind()) {
			return false
		}
		n := numericOf(v)
		switch r.key {
		case "gt":
			return n > r.num
		case "gte":
			return n >= r.num
		case "lt":
			return n < r.num
		default:
			return n <= r.num
		}
	case "oneof":
		s := fmt.Sprintf("%v", v.Interface())
		for _, a := range r.args {
			if s == a {
				return true
			}
		}
		return false
	case "email":
		if v.Kind() != reflect.String {
			return false
		}
		return emailRe.MatchString(v.String())
	default:
		return false
	}
}

func isNumericKind(k reflect.Kind) bool {
	return isIntKind(k) || isUintKind(k) || isFloatKind(k)
}

// numericOf 把数值类型规约成 float64 做比较：整数域无损（≤2^53），
// 浮点域保留小数——避免 int64 截断导致 gt=1.5/min=0.5 这类阈值算错。
func numericOf(v reflect.Value) float64 {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	}
	return 0
}

// isZero 判定校验语境下的零值（语义对齐 go-playground/validator 的 required）。
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func, reflect.Map:
		return v.IsNil()
	case reflect.Slice:
		return v.IsNil() || v.Len() == 0
	case reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Struct:
		return v.Interface() == reflect.Zero(v.Type()).Interface()
	default:
		return false
	}
}
