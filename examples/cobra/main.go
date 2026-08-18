// 迁移示例：已有 Cobra 工程时，把 xyz-go 的定义渲染成 Cobra 命令树。
//
// 这是三种共存方式中的「全替代」：保留你的 Cobra 架构、帮助样式与扩展，
// 只复用 xyz-go 的定义层（Entry.Root 元数据）与调用脊柱（Entry.Invoke）。
//
//	go run . add bob -a 20        # 短名/位置参数/描述全部来自 xyz 定义
//	APP_KEY=k go run . add alice  # env 注入照常（json:"-" 字段不走 flag）
package main

import (
	"context"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ejfkdev/xyz-go/cli"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

type AddArgs struct {
	Name string   `json:"name" desc:"用户名" required:"true" validate:"min=2" cli:"positional"`
	Age  int      `json:"age" desc:"年龄" default:"18" cli:"shorthand=a"`
	Tags []string `json:"tags" desc:"标签"`
	Key  string   `json:"-" secret:"true" desc:"密钥" cli:"env=APP_KEY"`
}

func add(_ context.Context, in *AddArgs) (*AddArgs, error) { return in, nil }

// entryToCobra 把一条 xyz 定义映射成 cobra.Command。要点：
//  1. 短名/位置参数/env/描述来自 Entry.Root（与 xyz CLI 前端同源元数据）；
//  2. flag 值归约成 map[string]any 后交给 Entry.Invoke——这是所有适配器的通用脊柱；
//  3. 渲染用 xyz/cli.Render，也可换成你自己的格式。
func entryToCobra(e *spec.Entry) *cobra.Command {
	segs := strings.Split(e.Name, ".")
	cmd := &cobra.Command{
		Use:     segs[len(segs)-1],
		Short:   e.Summary,
		Aliases: e.CLI.Aliases,
	}
	var positionals []*spec.FieldMeta
	var envFields []*spec.FieldMeta
	values := map[string]any{} // CLI 专属默认值铺底（与 xyz CLI 前端注入语义一致）
	for _, f := range e.Root.Fields {
		if f.Skip {
			if f.CLI.EnvVar != "" {
				envFields = append(envFields, f)
			}
			continue
		}
		if f.CLI.Positional {
			positionals = append(positionals, f)
			continue
		}
		if d := f.CLI.Default; d != nil {
			values[f.JSONName] = d
		}
	}

	flagTargets := map[string]any{}
	for _, f := range e.Root.Fields {
		if f.Skip || f.CLI.Positional {
			continue
		}
		fs := cmd.Flags()
		name := f.JSONName
		switch {
		case f.Kind == reflect.Bool:
			var b bool
			fs.BoolVarP(&b, name, f.CLI.Shorthand, false, f.Description)
			flagTargets[name] = &b
		case f.Kind == reflect.Slice && f.Type != reflect.TypeOf([]byte(nil)):
			var sv []string
			fs.StringSliceVarP(&sv, name, f.CLI.Shorthand, nil, f.Description)
			flagTargets[name] = &sv
		default:
			var sv string
			fs.StringVarP(&sv, name, f.CLI.Shorthand, "", f.Description)
			flagTargets[name] = &sv
		}
	}

	// 位置参数数量：required 前缀（与 xyz 前端同一约束）。
	minPos := 0
	allReq := true
	for i, f := range positionals {
		if f.Required && allReq {
			minPos = i + 1
		} else {
			allReq = false
		}
	}
	cmd.Args = cobra.RangeArgs(minPos, len(positionals))

	cmd.RunE = func(c *cobra.Command, args []string) error {
		m := map[string]any{}
		for k, v := range values {
			m[k] = v
		}
		for name, target := range flagTargets {
			if !c.Flags().Changed(name) {
				continue
			}
			switch p := target.(type) {
			case *bool:
				m[name] = *p
			case *[]string:
				m[name] = *p
			case *string:
				m[name] = *p
			}
		}
		for _, f := range envFields {
			if ev, ok := os.LookupEnv(f.CLI.EnvVar); ok && ev != "" {
				m[f.Name] = ev // json:"-" 字段按 Go 字段名注入
			}
		}
		for i, f := range positionals {
			if i < len(args) {
				m[f.JSONName] = args[i]
			}
		}
		out, err := e.Invoke(c.Context(), m)
		if err != nil {
			return err
		}
		return cli.Render(c.OutOrStdout(), out)
	}
	return cmd
}

func main() {
	reg := registry.New()
	spec.Define("user.add", add).
		Summary("添加用户").
		CLI(spec.CliHints{Usage: "add <name>", Aliases: []string{"ua"}}).
		Register(reg)

	root := cobra.Command{Use: "cobrademo"}
	for _, e := range reg.All() {
		root.AddCommand(entryToCobra(e))
	}
	_ = root.Execute()
}
