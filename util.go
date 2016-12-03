package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gx "github.com/whyrusleeping/gx/gxutil"
)

func GxDvcsImport(pkg *gx.Package) string {
	pkggx := make(map[string]interface{})
	_ = json.Unmarshal(pkg.Gx, &pkggx)
	return pkggx["dvcsimport"].(string)
}

func PkgDir(pkg *gx.Package) (string, error) {
	dir, err := gx.InstallPath(pkg.Language, "", true)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, GxDvcsImport(pkg)), nil
}

func EnumerateDependencies(pkg *gx.Package) (map[string]*gx.Package, error) {
	deps := make(map[string]*gx.Package)
	err := enumerateDepsRec(pkg, deps)
	if err != nil {
		return nil, err
	}

	return deps, nil
}

func enumerateDepsRec(pkg *gx.Package, deps map[string]*gx.Package) error {
	for _, d := range pkg.Dependencies {
		if _, ok := deps[d.Hash]; ok {
			continue
		}

		var dpkg gx.Package
		err := gx.LoadPackage(&dpkg, pkg.Language, d.Hash)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("package %s @ %s not found", d.Name, d.Hash)
			}
			return err
		}

		deps[d.Hash] = &dpkg

		err = enumerateDepsRec(&dpkg, deps)
		if err != nil {
			return err
		}
	}
	return nil
}
