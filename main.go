package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cli "github.com/codegangsta/cli"
	gx "github.com/whyrusleeping/gx/gxutil"
	. "github.com/whyrusleeping/stump"
)

var cwd string

func main() {
	app := cli.NewApp()
	app.Name = "gx-workspace"
	app.Author = "lgierth"
	app.Usage = "Tool for management of gx dependency trees"
	app.Version = "0.0.0"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose",
			Usage: "turn on verbose output",
		},
	}
	app.Before = func(c *cli.Context) error {
		Verbose = c.Bool("verbose")
		return nil
	}

	mcwd, err := os.Getwd()
	if err != nil {
		Fatal("failed to get cwd:", err)
	}
	lcwd, err := filepath.EvalSymlinks(mcwd)
	if err != nil {
		Fatal("failed to resolve symlinks of cdw:", err)
	}
	cwd = lcwd

	app.Commands = []cli.Command{
		PullCommand,
		BubbleListCommand,
		// UpdateCommand,
		// TestCommand,
		ExecCommand,
	}

	if err := app.Run(os.Args); err != nil {
		Fatal(err)
	}
}

var BubbleListCommand = cli.Command{
	Name:  "bubble-list",
	Usage: "list all packages affected by an update of the named package",
	Action: func(c *cli.Context) error {
		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		if !c.Args().Present() {
			return fmt.Errorf("must pass a package name")
		}

		upd := c.Args().First()

		var touched []string
		memo := make(map[string]bool)

		var checkRec func(pkg *gx.Package) (bool, error)
		checkRec = func(pkg *gx.Package) (bool, error) {
			var needsUpd bool
			pkg.ForEachDep(func(dep *gx.Dependency, pkg *gx.Package) error {
				val, ok := memo[dep.Hash]
				if dep.Name == upd {
					needsUpd = true
				} else {
					if ok {
						needsUpd = val || needsUpd
					} else {
						nu, err := checkRec(pkg)
						if err != nil {
							return err
						}

						memo[dep.Hash] = nu
						needsUpd = nu || needsUpd
					}
				}
				return nil
			})
			if needsUpd {
				touched = append(touched, pkg.Name)
			}
			return needsUpd, nil
		}

		needs, err := checkRec(&pkg)
		if err != nil {
			return err
		}

		if !needs {
			fmt.Println("named package not in dependency tree")
		}

		for _, p := range touched {
			fmt.Println(p)
		}
		return nil
	},
}

var PullCommand = cli.Command{
	Name:  "pull",
	Usage: "fetches each dependency's git repo by running go get -d",
	Action: func(c *cli.Context) error {
		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		deps, err := EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		Log("pulling %d package repositories...", len(deps))

		for h, dpkg := range deps {
			dvcsimport := GxDvcsImport(dpkg)
			if dvcsimport == "" {
				return fmt.Errorf("package %s @ %s doesn't have gx.dvcsimport set", dpkg.Name, h)
			}

			// TODO: these won't go-get
			//       can't load package: package github.com/gogo/protobuf: no buildable Go source files
			if strings.HasPrefix(dvcsimport, "golang.org/x/") {
				continue
			}
			if dvcsimport == "github.com/gogo/protobuf" {
				continue
			}

			// TODO: add option for passing -u, -v
			VLog("go get -d %s", dvcsimport)
			cmd := exec.Command("go", "get", "-d", dvcsimport)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err = cmd.Run(); err != nil {
				return err
			}
		}

		Log("done.")

		return nil
	},
}

var ExecCommand = cli.Command{
	Name:  "exec",
	Usage: "executes the given command in each dependency repo",
	Action: func(c *cli.Context) error {
		if len(c.Args()) == 0 {
			return fmt.Errorf("exec requires a command to run")
		}
		cmd := c.Args().First()

		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		deps, err := EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		Log("executing in %d package repositories...", len(deps))

		for _, dpkg := range deps {
			dir, err := PkgDir(dpkg)
			if err != nil {
				return err
			}

			VLog("sh -c '%s'", cmd)
			cmd := exec.Command("sh", "-c", cmd)
			cmd.Dir = dir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err = cmd.Run(); err != nil {
				return err
			}
		}

		Log("done.")

		return nil
	},
}

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
