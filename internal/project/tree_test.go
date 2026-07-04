package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildFileTree_NestedSortedDirsFirst(t *testing.T) {
	root := t.TempDir()
	makeTreeFixture(t, root)

	tree, err := BuildFileTree(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildFileTree: %v", err)
	}

	if tree.Name != filepath.Base(root) {
		t.Errorf("root Name = %q, want %q", tree.Name, filepath.Base(root))
	}
	if tree.RelPath != "." {
		t.Errorf("root RelPath = %q, want %q", tree.RelPath, ".")
	}
	if !tree.IsDir {
		t.Error("root IsDir = false, want true")
	}
	if !pathsEqual(tree.Path, root) {
		t.Errorf("root Path = %q, want %q", tree.Path, root)
	}

	want := []string{"Alpha", "zeta", "A.txt", "b.txt", "main.go", "README"}
	if got := treeNames(tree); !reflect.DeepEqual(got, want) {
		t.Fatalf("root children = %v, want %v", got, want)
	}

	alpha := findChild(t, tree, "Alpha")
	if !alpha.IsDir {
		t.Error("Alpha IsDir = false, want true")
	}
	deep := findChild(t, alpha, "deep")
	if deep.RelPath != "Alpha/deep" {
		t.Errorf("deep RelPath = %q, want %q", deep.RelPath, "Alpha/deep")
	}
	if wantPath := filepath.Join(root, "Alpha", "deep"); !pathsEqual(deep.Path, wantPath) {
		t.Errorf("deep Path = %q, want %q", deep.Path, wantPath)
	}
	leaf := findChild(t, deep, "leaf.txt")
	if leaf.RelPath != "Alpha/deep/leaf.txt" {
		t.Errorf("leaf RelPath = %q, want %q", leaf.RelPath, "Alpha/deep/leaf.txt")
	}
	if leaf.IsDir {
		t.Error("leaf IsDir = true, want false")
	}
	if leaf.Children != nil {
		t.Errorf("leaf Children = %v, want nil", leaf.Children)
	}
}

func TestBuildFileTree_SkipRules(t *testing.T) {
	root := t.TempDir()
	skipped := []string{".git", ".gomodcache", "bin", "dist", "node_modules", "vendor"}
	for _, name := range skipped {
		dir := filepath.Join(root, name)
		makeDir(t, dir)
		writeFile(t, filepath.Join(dir, "inside.txt"), "x\n")
	}
	writeFile(t, filepath.Join(root, ".gitignore"), "bin/\n")
	makeDir(t, filepath.Join(root, "src"))
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "keep.txt"), "x\n")

	tree, err := BuildFileTree(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildFileTree: %v", err)
	}

	want := []string{"src", "keep.txt"}
	if got := treeNames(tree); !reflect.DeepEqual(got, want) {
		t.Fatalf("root children = %v, want %v", got, want)
	}
	for _, name := range append(skipped, ".gitignore") {
		t.Run(name, func(t *testing.T) {
			for _, c := range tree.Children {
				if c.Name == name {
					t.Errorf("skipped entry %q present in tree", name)
				}
			}
		})
	}
}

func TestBuildFileTree_ContextCanceled(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "x\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := BuildFileTree(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildFileTree err = %v, want context.Canceled", err)
	}
}

func TestBuildFileTree_IncludesNonGoFiles(t *testing.T) {
	root := t.TempDir()
	files := []string{"README.md", "go.mod", "image.png", "notes.txt"}
	for _, name := range files {
		writeFile(t, filepath.Join(root, name), "x\n")
	}

	tree, err := BuildFileTree(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildFileTree: %v", err)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			c := findChild(t, tree, name)
			if c.IsDir {
				t.Errorf("%q IsDir = true, want false", name)
			}
		})
	}
}

func TestBuildFileTree_EmptyRoot(t *testing.T) {
	root := t.TempDir()

	tree, err := BuildFileTree(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildFileTree: %v", err)
	}
	if tree.Name != filepath.Base(root) {
		t.Errorf("Name = %q, want %q", tree.Name, filepath.Base(root))
	}
	if tree.RelPath != "." {
		t.Errorf("RelPath = %q, want %q", tree.RelPath, ".")
	}
	if !tree.IsDir {
		t.Error("IsDir = false, want true")
	}
	if len(tree.Children) != 0 {
		t.Errorf("Children = %v, want none", tree.Children)
	}
}

func TestBuildFileTree_DoubleBuildDeterministic(t *testing.T) {
	root := t.TempDir()
	makeTreeFixture(t, root)

	first, err := BuildFileTree(context.Background(), root)
	if err != nil {
		t.Fatalf("first BuildFileTree: %v", err)
	}
	second, err := BuildFileTree(context.Background(), root)
	if err != nil {
		t.Fatalf("second BuildFileTree: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("trees differ between builds:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func TestBuildFileTree_UnreadableRoot_ErrTreeRoot(t *testing.T) {
	cases := []struct {
		name string
		root func(t *testing.T) string
	}{
		{
			name: "missing directory",
			root: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "absent")
			},
		},
		{
			name: "regular file",
			root: func(t *testing.T) string {
				t.Helper()
				p := filepath.Join(t.TempDir(), "f.txt")
				writeFile(t, p, "x\n")
				return p
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildFileTree(context.Background(), tc.root(t)); !errors.Is(err, ErrTreeRoot) {
				t.Fatalf("BuildFileTree err = %v, want ErrTreeRoot", err)
			}
		})
	}
}

func TestProject_FileTree_WalksRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")

	p := &Project{root: root}
	tree, err := p.FileTree(context.Background())
	if err != nil {
		t.Fatalf("FileTree: %v", err)
	}
	if !pathsEqual(tree.Path, root) {
		t.Errorf("root Path = %q, want %q", tree.Path, root)
	}
	findChild(t, tree, "main.go")
}

func TestCompareTreeNodes_Ordering(t *testing.T) {
	// Exercises the comparator directly: a case-insensitive filesystem
	// (e.g., NTFS) cannot host names differing only by case side by side.
	cases := []struct {
		name string
		a, b TreeNode
		want int // sign of the comparison result
	}{
		{"dir before file", TreeNode{Name: "zzz", IsDir: true}, TreeNode{Name: "aaa"}, -1},
		{"file after dir", TreeNode{Name: "aaa"}, TreeNode{Name: "zzz", IsDir: true}, 1},
		{"case-insensitive order", TreeNode{Name: "Beta"}, TreeNode{Name: "alpha"}, 1},
		{"tie broken case-sensitively", TreeNode{Name: "Foo"}, TreeNode{Name: "foo"}, -1},
		{"equal names", TreeNode{Name: "same"}, TreeNode{Name: "same"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareTreeNodes(tc.a, tc.b); intSign(got) != tc.want {
				t.Errorf("compareTreeNodes(%q, %q) = %d, want sign %d", tc.a.Name, tc.b.Name, got, tc.want)
			}
		})
	}
}

// makeTreeFixture populates root with a small mixed tree shared by tests:
// two directories (one nested two levels) and four loose files whose names
// exercise the case-insensitive ordering.
func makeTreeFixture(t *testing.T, root string) {
	t.Helper()
	makeDir(t, filepath.Join(root, "zeta"))
	makeDir(t, filepath.Join(root, "Alpha", "deep"))
	writeFile(t, filepath.Join(root, "Alpha", "deep", "leaf.txt"), "x\n")
	writeFile(t, filepath.Join(root, "zeta", "inner.md"), "x\n")
	writeFile(t, filepath.Join(root, "README"), "x\n")
	writeFile(t, filepath.Join(root, "b.txt"), "x\n")
	writeFile(t, filepath.Join(root, "A.txt"), "x\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
}

// makeDir creates dir and any missing parents.
func makeDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// treeNames returns the ordered names of node's direct children.
func treeNames(node TreeNode) []string {
	names := make([]string, len(node.Children))
	for i, c := range node.Children {
		names[i] = c.Name
	}
	return names
}

// findChild returns node's direct child with the given name, failing the
// test if it is absent.
func findChild(t *testing.T, node TreeNode, name string) TreeNode {
	t.Helper()
	for _, c := range node.Children {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("node %s has no child %q (children: %v)", node.RelPath, name, treeNames(node))
	return TreeNode{}
}

// intSign normalizes a comparison result to -1, 0, or 1.
func intSign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
