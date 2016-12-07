package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	semver "github.com/blang/semver"
	gx "github.com/whyrusleeping/gx/gxutil"
)

// TODO: make sure all callers check for empty result
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
	imp := GxDvcsImport(pkg)
	if imp == "" {
		return "", fmt.Errorf("package %s doesn't have gx.dvcsimport set", pkg.Name)
	}
	return filepath.Join(dir, imp), nil
}

type pkgInWorkspace struct {
	Dir  string
	Pkg  *gx.Package
	Deps map[string]bool
}

func EnumerateDependencies(pkg *gx.Package) (map[string]pkgInWorkspace, error) {
	deps := make(map[string]pkgInWorkspace)
	err := enumerateDepsRec(pkg, deps)
	if err != nil {
		return nil, err
	}

	return deps, nil
}

func enumerateDepsRec(pkg *gx.Package, deps map[string]pkgInWorkspace) error {
	dir, err := PkgDir(pkg)
	if err != nil {
		return err
	}

	if _, found := deps[pkg.Name]; found {
		return nil
	}
	deps[pkg.Name] = pkgInWorkspace{
		Dir:  dir,
		Pkg:  pkg,
		Deps: map[string]bool{},
	}

	for _, d := range pkg.Dependencies {
		var dpkg gx.Package
		err := gx.LoadPackage(&dpkg, pkg.Language, d.Hash)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("package %s @ %s not found", d.Name, d.Hash)
			}
			return err
		}

		deps[pkg.Name].Deps[dpkg.Name] = true

		err = enumerateDepsRec(&dpkg, deps)
		if err != nil {
			return err
		}
	}
	return nil
}

func makePM() (*gx.PM, error) {
	cfg, err := gx.LoadConfig()
	if err != nil {
		return nil, err
	}

	pm, err := gx.NewPM(cfg)
	if err != nil {
		return nil, err
	}

	return pm, nil
}

// finds a package in `round` which doesn't depend on any other packages in `round`.
func pkgFromStack(stack map[string]bool, pkgs map[string]pkgInWorkspace) (string, error) {
	for p, _ := range stack {
		_, found := pkgs[p]
		if !found {
			return "", fmt.Errorf("unknown package in this round: %s", p)
		}

		depsInThisRound := false
		for tr, _ := range stack {
			_, ok := pkgs[p].Deps[tr]
			if ok {
				depsInThisRound = true
				break
			}
		}
		if !depsInThisRound {
			return p, nil
		}
	}

	return "", fmt.Errorf("circular dependencies in %+v", stack)
}

func updateVersion(pkg *gx.Package, nver string) (outerr error) {
	if nver == "" {
		return fmt.Errorf("must specify version with non-zero length")
	}

	// defer func() {
	// 	err := gx.SavePackageFile(pkg, gx.PkgFileName)
	// 	if err != nil {
	// 		outerr = err
	// 	}
	// }()

	// if argument is a semver, set version to it
	_, err := semver.Make(nver)
	if err == nil {
		pkg.Version = nver
		return nil
	}

	v, err := semver.Make(pkg.Version)
	if err != nil {
		return err
	}
	switch nver {
	case "major":
		v.Major++
		v.Minor = 0
		v.Patch = 0
		v.Pre = nil // reset prerelase info
	case "minor":
		v.Minor++
		v.Patch = 0
		v.Pre = nil
	case "patch":
		v.Patch++
		v.Pre = nil
	default:
		if nver[0] == 'v' {
			nver = nver[1:]
		}
		newver, err := semver.Make(nver)
		if err != nil {
			return err
		}
		v = newver
	}

	pkg.Version = v.String()

	return nil
}

func doPublish(pm *gx.PM, dir string, pkg *gx.Package) (string, error) {
	err := gx.TryRunHook("pre-publish", pkg.Language, pkg.SubtoolRequired)
	if err != nil {
		return "", err
	}

	hash, err := pm.PublishPackage(dir, &pkg.PackageBase)
	if err != nil {
		return "", fmt.Errorf("publish %s: %s", dir, err)
	}

	// err = writeLastPub(dir, pkg.Version, hash)
	// if err != nil {
	// 	return "", err
	// }

	err = gx.TryRunHook("post-publish", pkg.Language, pkg.SubtoolRequired, hash)
	if err != nil {
		return "", err
	}

	return hash, nil
}

func writeLastPub(dir, vers string, hash string) error {
	err := os.MkdirAll(filepath.Join(dir, ".gx"), 0755)
	if err != nil {
		return err
	}

	fp := filepath.Join(dir, ".gx/lastpubver")
	fi, err := os.Create(fp)
	if err != nil {
		return fmt.Errorf("create lastpubver: %s", err)
	}

	defer fi.Close()

	_, err = fmt.Fprintf(fi, "%s: %s\n", vers, hash)
	if err != nil {
		return fmt.Errorf("failed to write version file: %s", err)
	}

	return nil
}
