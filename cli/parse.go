package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ejfkdev/xyz-go/spec"
)

type flagKind int

const (
	fStr flagKind = iota
	fBool
	fSlice
)

// flagDef 描述一个长/短 flag 的定义与取值方式。
type flagDef struct {
	long  string
	short string
	kind  flagKind
	field *spec.FieldMeta
}

// flagVal 是一次解析中某个 flag 的取值状态。
type flagVal struct {
	seen    bool
	str     string
	list    []string
	boolean bool
}

// cmdNode 是命令树的一个段（叶子才是可执行命令）。
func parseFlags(defs []flagDef, args []string) ([]flagVal, []string, error) {
	longIdx := map[string]int{}
	shortIdx := map[string]int{}
	for i := range defs {
		longIdx[defs[i].long] = i
		if defs[i].short != "" {
			shortIdx[defs[i].short] = i
		}
	}
	fvals := make([]flagVal, len(defs))
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
			continue
		case strings.HasPrefix(a, "--"):
			name, val, hasVal := strings.Cut(a[2:], "=")
			di, ok := longIdx[name]
			if !ok {
				return nil, nil, fmt.Errorf("unknown flag: --%s", name)
			}
			d := &defs[di]
			fv := &fvals[di]
			if d.kind == fBool {
				b := true
				if hasVal {
					var err error
					b, err = strconv.ParseBool(val)
					if err != nil {
						return nil, nil, fmt.Errorf("invalid boolean value %q for --%s", val, name)
					}
				}
				fv.seen = true
				fv.boolean = b
				continue
			}
			if !hasVal {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("flag needs an argument: --%s", name)
				}
				i++
				val = args[i]
			}
			fv.seen = true
			if d.kind == fSlice {
				fv.list = append(fv.list, val)
			} else {
				fv.str = val
			}
			continue
		case strings.HasPrefix(a, "-") && len(a) > 1:
			di, ok := shortIdx[a[1:2]]
			if !ok {
				return nil, nil, fmt.Errorf("unknown shorthand flag: -%s", a[1:2])
			}
			d := &defs[di]
			fv := &fvals[di]
			rest := a[2:]
			if strings.HasPrefix(rest, "=") {
				rest = rest[1:]
			}
			if d.kind == fBool {
				if rest == "" {
					fv.seen, fv.boolean = true, true
					continue
				}
				b, err := strconv.ParseBool(rest)
				if err != nil {
					return nil, nil, fmt.Errorf("invalid boolean value %q for -%s", rest, d.short)
				}
				fv.seen, fv.boolean = true, b
				continue
			}
			if rest == "" {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("flag needs an argument: -%s", d.short)
				}
				i++
				rest = args[i]
			}
			fv.seen = true
			if d.kind == fSlice {
				fv.list = append(fv.list, rest)
			} else {
				fv.str = rest
			}
			continue
		default:
			pos = append(pos, a)
		}
	}
	return fvals, pos, nil
}

// printHelp 输出节点帮助：父节点列子命令，叶子列 flags 与用法。
