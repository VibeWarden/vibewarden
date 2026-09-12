// Package architecture_test — streaming-safety invariant for ResponseWriter wrappers.
//
// TestResponseWriterWrappers_ForwardFlushAndUnwrap enforces that every struct in
// internal/ that embeds http.ResponseWriter also declares Flush() and
// Unwrap() http.ResponseWriter. Embedding alone hides the underlying
// http.Flusher, which buffers streaming responses (text/event-stream, chunked
// LLM output) until the connection closes — the defect fixed in #1526.
package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// requiredWrapperMethods are the methods a ResponseWriter wrapper must declare
// in addition to whatever it overrides for its own purposes.
var requiredWrapperMethods = []string{"Flush", "Unwrap"}

func TestResponseWriterWrappers_ForwardFlushAndUnwrap(t *testing.T) {
	root := filepath.Join("..", "..", "internal")

	// embedders maps "<dir>.<TypeName>" to the file that declares it.
	embedders := map[string]string{}
	// methods maps "<dir>.<TypeName>" to the set of method names declared on it.
	methods := map[string]map[string]bool{}

	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		dir := filepath.Dir(path)

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || !embedsResponseWriter(st) {
						continue
					}
					embedders[dir+"."+ts.Name.Name] = path
				}
			case *ast.FuncDecl:
				recv := receiverTypeName(d)
				if recv == "" {
					continue
				}
				key := dir + "." + recv
				if methods[key] == nil {
					methods[key] = map[string]bool{}
				}
				methods[key][d.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if len(embedders) == 0 {
		t.Fatal("found no structs embedding http.ResponseWriter: the scan is broken")
	}

	for key, file := range embedders {
		for _, method := range requiredWrapperMethods {
			if !methods[key][method] {
				t.Errorf("%s (%s) embeds http.ResponseWriter but does not declare %s(); "+
					"streaming responses through it would be buffered until the connection closes (see #1526)",
					strings.TrimPrefix(key, filepath.Join("..", "..")+string(filepath.Separator)), file, method)
			}
		}
	}
}

// embedsResponseWriter reports whether the struct has an embedded
// http.ResponseWriter field.
func embedsResponseWriter(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		if len(field.Names) != 0 {
			continue // named field, not embedded
		}
		sel, ok := field.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		if pkg.Name == "http" && sel.Sel.Name == "ResponseWriter" {
			return true
		}
	}
	return false
}

// receiverTypeName returns the base type name of a method receiver, or "" when
// the declaration is a plain function.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}
