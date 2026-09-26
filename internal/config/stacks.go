package config

// StackDefaults son los valores que un stack aporta cuando el repo no los define.
type StackDefaults struct {
	CodePaths    []string
	TestPatterns []string
	UI           bool
}

var stacks = map[string]StackDefaults{
	"go": {
		CodePaths:    []string{"cmd", "internal", "pkg", "go.mod", "go.sum"},
		TestPatterns: []string{"*_test.go", "testdata/**"},
	},
	"angular": {
		CodePaths:    []string{"src"},
		TestPatterns: []string{"*.spec.ts", "e2e/**"},
		UI:           true,
	},
	"node": {
		CodePaths:    []string{"src"},
		TestPatterns: []string{"*.test.ts", "*.test.js", "*.spec.ts", "*.spec.js"},
	},
}

// Stack devuelve los defaults de un stack conocido.
func Stack(name string) (StackDefaults, bool) {
	s, ok := stacks[name]
	return s, ok
}
