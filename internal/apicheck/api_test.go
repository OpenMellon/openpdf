package apicheck

import (
	"flag"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

var update = flag.Bool("update", false, "rewrite testdata/api.txt")

const golden = "testdata/api.txt"

func qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Path()
}

// tuple drops parameter and result names so renaming one does not change the recorded signature.
func tuple(t *types.Tuple) *types.Tuple {
	vars := make([]*types.Var, t.Len())
	for i := range vars {
		vars[i] = types.NewVar(0, nil, "", t.At(i).Type())
	}
	return types.NewTuple(vars...)
}

func signature(s *types.Signature) string {
	var params []*types.TypeParam
	for tparam := range s.TypeParams().TypeParams() {
		params = append(params, tparam)
	}
	normalized := types.NewSignatureType(nil, nil, params, tuple(s.Params()), tuple(s.Results()), s.Variadic())
	return types.TypeString(normalized, qualifier)
}

// apiLines returns one sorted line per exported declaration in every package of the module.
func apiLines(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &packages.Config{
		Dir:   root,
		Tests: false,
		Mode:  packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			t.Errorf("%s: %v", p.PkgPath, e)
			failed = true
		}
	})
	if failed {
		t.FailNow()
	}
	var lines []string
	for _, p := range pkgs {
		if p.Types == nil {
			continue
		}
		prefix := p.PkgPath + " "
		for _, name := range p.Types.Scope().Names() {
			obj := p.Types.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			switch o := obj.(type) {
			case *types.Func:
				lines = append(lines, prefix+"func "+name+" "+signature(o.Type().(*types.Signature)))
			case *types.TypeName:
				lines = append(lines, typeLines(prefix, name, o)...)
			case *types.Const:
				lines = append(lines, fmt.Sprintf("%sconst %s %s = %s", prefix, name, types.TypeString(o.Type(), qualifier), o.Val().ExactString()))
			default:
				lines = append(lines, prefix+types.ObjectString(obj, qualifier))
			}
		}
	}
	sort.Strings(lines)
	return lines
}

func typeLines(prefix, name string, o *types.TypeName) []string {
	lines := []string{prefix + "type " + name}
	named, ok := o.Type().(*types.Named)
	if !ok {
		return append(lines, prefix+"alias "+name+" "+types.TypeString(o.Type(), qualifier))
	}
	if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
		lines = append(lines, prefix+"underlying "+name+" "+types.TypeString(named.Underlying(), qualifier))
	}
	if tp := named.TypeParams(); tp.Len() > 0 {
		var params []string
		for tparam := range tp.TypeParams() {
			params = append(params, tparam.Obj().Name()+" "+types.TypeString(tparam.Constraint(), qualifier))
		}
		lines = append(lines, prefix+"typeparams "+name+" "+strings.Join(params, ", "))
	}
	for _, typ := range []types.Type{named, types.NewPointer(named)} {
		methods := types.NewMethodSet(typ)
		for method := range methods.Methods() {
			m := method.Obj()
			if m.Exported() {
				lines = append(lines, prefix+"method "+types.TypeString(typ, qualifier)+" "+m.Name()+" "+signature(m.Type().(*types.Signature)))
			}
		}
	}
	if st, ok := named.Underlying().(*types.Struct); ok {
		// Order matters to unkeyed composite literals, but the sorted listing loses it, so record it on one line.
		var order []string
		for field := range st.Fields() {
			order = append(order, field.Name())
		}
		lines = append(lines, prefix+"fieldorder "+name+" "+strings.Join(order, ","))
		for i := 0; i < st.NumFields(); i++ {
			if f := st.Field(i); f.Exported() {
				lines = append(lines, fmt.Sprintf("%sfield %s %s %s %q", prefix, name, f.Name(), types.TypeString(f.Type(), qualifier), st.Tag(i)))
			}
		}
	}
	return lines
}

func TestAPI(t *testing.T) {
	got := strings.Join(apiLines(t), "\n") + "\n"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v; run go test ./internal/apicheck -update", err)
	}
	if string(want) == got {
		return
	}
	wantSet, gotSet := lineSet(string(want)), lineSet(got)
	var removed, added []string
	for l := range wantSet {
		if !gotSet[l] {
			removed = append(removed, l)
		}
	}
	for l := range gotSet {
		if !wantSet[l] {
			added = append(added, l)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	t.Errorf("exported API changed: %d removed, %d added; run go test ./internal/apicheck -update if intended\n%s%s",
		len(removed), len(added), sample("removed", removed), sample("added", added))
}

func lineSet(s string) map[string]bool {
	m := map[string]bool{}
	for l := range strings.SplitSeq(strings.TrimSuffix(s, "\n"), "\n") {
		m[l] = true
	}
	return m
}

func sample(label string, lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i == 20 {
			fmt.Fprintf(&b, "  ... %d more %s\n", len(lines)-20, label)
			break
		}
		fmt.Fprintf(&b, "  %s: %s\n", label, l)
	}
	return b.String()
}
