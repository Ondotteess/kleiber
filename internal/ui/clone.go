package ui

func cloneState(src State) State {
	out := State{
		Commands: cloneCommands(src.Commands),
		Buffers:  cloneBuffers(src.Buffers),
		Views:    cloneViews(src.Views),
		Project:  cloneProject(src.Project),
	}
	return out
}

func cloneCommands(src []CommandItem) []CommandItem {
	if src == nil {
		return nil
	}
	out := make([]CommandItem, len(src))
	copy(out, src)
	return out
}

func cloneBuffers(src []BufferItem) []BufferItem {
	if src == nil {
		return nil
	}
	out := make([]BufferItem, len(src))
	copy(out, src)
	return out
}

func cloneViews(src []ViewItem) []ViewItem {
	if src == nil {
		return nil
	}
	out := make([]ViewItem, len(src))
	copy(out, src)
	return out
}

func cloneProject(src ProjectState) ProjectState {
	out := ProjectState{
		Open:     src.Open,
		Root:     src.Root,
		Modules:  cloneModules(src.Modules),
		Packages: clonePackages(src.Packages),
	}
	return out
}

func cloneModules(src []ModuleItem) []ModuleItem {
	if src == nil {
		return nil
	}
	out := make([]ModuleItem, len(src))
	copy(out, src)
	return out
}

func clonePackages(src []PackageItem) []PackageItem {
	if src == nil {
		return nil
	}
	out := make([]PackageItem, len(src))
	for i, pkg := range src {
		out[i] = PackageItem{
			ImportPath: pkg.ImportPath,
			Dir:        pkg.Dir,
			RelDir:     pkg.RelDir,
			Files:      cloneFiles(pkg.Files),
			TestFiles:  cloneFiles(pkg.TestFiles),
		}
	}
	return out
}

func cloneFiles(src []FileItem) []FileItem {
	if src == nil {
		return nil
	}
	out := make([]FileItem, len(src))
	copy(out, src)
	return out
}
