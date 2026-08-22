package cli

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

type cmdNode struct {
	path     string // 完整点分路径，如 user.add
	usage    string
	short    string
	long     string
	aliases  []string
	hidden   bool
	leaf     bool
	entry    *spec.Entry
	fields   []*spec.FieldMeta
	defs     []flagDef
	envOnly  []*spec.FieldMeta // json:"-" 且配置了 env 的注入字段
	posF     []*spec.FieldMeta
	minPos   int
	maxPos   int
	children map[string]*cmdNode
	order    []string // children 排序后的名字
	dflt     *cmdNode // 默认子命令（未匹配命令段时整串参数转发给它）
}

// App is the CLI frontend for one registry.
func New(reg *registry.Registry) (*App, error) {
	if reg == nil {
		return nil, fmt.Errorf("cli: nil registry")
	}
	a := &App{
		reg:    reg,
		root:   &cmdNode{children: map[string]*cmdNode{}},
		out:    os.Stdout,
		errOut: os.Stderr,
	}
	for _, e := range reg.All() {
		if err := a.addEntry(e); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *App) addEntry(e *spec.Entry) error {
	parts := strings.Split(e.Name, ".")
	node := a.root
	parent := a.root
	for _, part := range parts {
		parent = node
		child, ok := node.children[part]
		if !ok {
			child = &cmdNode{children: map[string]*cmdNode{}}
			node.children[part] = child
			node.order = append(node.order, part)
		}
		node = child
	}
	if node != a.root && (node.leaf || len(node.children) > 0) {
		return fmt.Errorf("cli: command %q conflicts with an existing command path", e.Name)
	}
	// 别名挂在父节点查找表上，与子命令名等价（不出现在父帮助的命令列表里）。
	for _, alias := range e.CLI.Aliases {
		if taken, ok := parent.children[alias]; ok && taken != node {
			return fmt.Errorf("cli: command %q: alias %q collides with an existing command path", e.Name, alias)
		}
		parent.children[alias] = node
	}

	node.path = e.Name
	node.usage = e.CLI.Usage
	node.short = e.Summary
	node.long = e.Description
	node.aliases = e.CLI.Aliases
	node.hidden = e.CLI.Hidden
	node.leaf = true
	node.entry = e

	seenSh := map[string]string{}
	for _, f := range e.Root.Fields {
		if f.CLI.Skip {
			continue
		}
		if f.Skip {
			// json:"-" 字段不生成 flag；配置了 env 时注册纯注入点。
			if f.CLI.EnvVar != "" {
				node.envOnly = append(node.envOnly, f)
			}
			continue
		}
		if f.CLI.Positional {
			node.posF = append(node.posF, f)
			continue
		}
		kind, ok, err := flagKindFor(f)
		if err != nil {
			return fmt.Errorf("cli: command %q: %w", e.Name, err)
		}
		if !ok {
			continue
		}
		if f.CLI.Shorthand != "" {
			if prev, dup := seenSh[f.CLI.Shorthand]; dup {
				return fmt.Errorf("cli: command %q: shorthand %q of field %q already used by %q",
					e.Name, f.CLI.Shorthand, f.JSONName, prev)
			}
			seenSh[f.CLI.Shorthand] = f.JSONName
		}
		node.defs = append(node.defs, flagDef{
			long: f.JSONName, short: f.CLI.Shorthand, kind: kind, field: f,
		})
	}

	// Default：登记为父节点的默认子命令，一个父节点最多一个。
	if e.CLI.Default {
		if parent.dflt != nil && parent.dflt != node {
			return fmt.Errorf("cli: command %q: default conflicts with existing default %q", e.Name, parent.dflt.path)
		}
		parent.dflt = node
	}

	// 位置参数：required 必须是前缀，否则语义有歧义。
	minPos := 0
	allRequired := true
	for i, f := range node.posF {
		if f.Required && !allRequired {
			return fmt.Errorf("cli: command %q: required positional %q must not follow optional ones", e.Name, f.JSONName)
		}
		if f.Required {
			minPos = i + 1
		} else {
			allRequired = false
		}
	}
	node.minPos = minPos
	node.maxPos = len(node.posF)
	sort.Strings(node.order)
	return nil
}

// flagKindFor 判定字段的 flag 类型；不支持的种类在构建时报错。
func flagKindFor(f *spec.FieldMeta) (flagKind, bool, error) {
	switch {
	case f.Kind == reflect.Bool:
		return fBool, true, nil
	case f.Kind == reflect.Slice && f.Type != reflect.TypeOf([]byte(nil)):
		if f.Elem.Kind == reflect.Struct || f.Elem.Kind == reflect.Ptr || f.Elem.Kind == reflect.Slice {
			return fStr, false, fmt.Errorf("field %q: slice of %s is not supported by the CLI frontend yet", f.JSONName, f.Elem.Type)
		}
		return fSlice, true, nil
	case f.Kind == reflect.Struct && f.Type != reflect.TypeOf(time.Time{}):
		return fStr, false, fmt.Errorf("field %q: nested struct is not supported by the CLI frontend yet", f.JSONName)
	case f.Kind == reflect.Ptr && f.Elem != nil && f.Elem.Kind == reflect.Struct && f.Elem.Type != reflect.TypeOf(time.Time{}):
		return fStr, false, fmt.Errorf("field %q: pointer to struct is not supported by the CLI frontend yet", f.JSONName)
	case f.Kind == reflect.Map || f.Kind == reflect.Chan || f.Kind == reflect.Func ||
		f.Kind == reflect.Interface || f.Kind == reflect.Complex64 || f.Kind == reflect.Complex128:
		return fStr, false, fmt.Errorf("field %q: kind %s is not supported by the CLI frontend", f.JSONName, f.Kind)
	default:
		return fStr, true, nil
	}
}

// Run parses args as the CLI subcommand form and returns the exit code:
// 0 on success, 2 for usage/flag errors, and for command failures the code
// mapped from the error's classification. -h/--help print help; -v/--version
// print the version and exit 0.
