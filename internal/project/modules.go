package project

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// loadModules populates p.modules. It prefers a go.work file at the
// project root (multi-module workspace); otherwise it walks up from root
// looking for a single go.mod.
func (p *Project) loadModules(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	goWork := filepath.Join(p.root, "go.work")
	if info, err := os.Stat(goWork); err == nil && !info.IsDir() {
		return p.loadWorkModules(ctx, goWork)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", goWork, err)
	}

	dir := p.root
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		goMod := filepath.Join(dir, "go.mod")
		info, err := os.Stat(goMod)
		switch {
		case err == nil && !info.IsDir():
			mod, parseErr := parseModFile(goMod)
			if parseErr != nil {
				return parseErr
			}
			p.modules = []Module{mod}
			return nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("stat %s: %w", goMod, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ErrNoModule
		}
		dir = parent
	}
}

func (p *Project) loadWorkModules(ctx context.Context, goWork string) error {
	data, err := os.ReadFile(goWork)
	if err != nil {
		return fmt.Errorf("reading %s: %w", goWork, err)
	}
	wf, err := modfile.ParseWork(goWork, data, nil)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", goWork, err)
	}
	mods := make([]Module, 0, len(wf.Use))
	for _, u := range wf.Use {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := u.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(goWork), dir)
		}
		mod, err := parseModFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}
		mods = append(mods, mod)
	}
	p.modules = mods
	return nil
}

func parseModFile(goMod string) (Module, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return Module{}, fmt.Errorf("reading %s: %w", goMod, err)
	}
	f, err := modfile.Parse(goMod, data, nil)
	if err != nil {
		return Module{}, fmt.Errorf("parsing %s: %w", goMod, err)
	}
	mod := Module{
		GoMod: goMod,
		Dir:   filepath.Dir(goMod),
	}
	if f.Module != nil {
		mod.Path = f.Module.Mod.Path
	}
	if f.Go != nil {
		mod.GoVersion = f.Go.Version
	}
	return mod, nil
}
