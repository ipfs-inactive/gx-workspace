package main

import (
	"bytes"
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
		DirsCommand,
		PullCommand,
		UpdateCommand,
		// TestCommand,
		ExecCommand,
	}

	if err := app.Run(os.Args); err != nil {
		Fatal(err)
	}
}

var DirsCommand = cli.Command{
	Name:  "dirs",
	Usage: "prints the paths of package repositories",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "dirty,d",
			Usage: "print only repositories with changes",
		},
	},
	Action: func(c *cli.Context) error {
		dirty := c.Bool("dirty")

		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		pkgs, err := EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		for _, pkg := range pkgs {
			cmd := exec.Command("git", "status", "--porcelain")
			cmd.Dir = pkg.Dir
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = os.Stderr
			if err = cmd.Run(); err != nil {
				Log("git status -s: %s", err)
			}

			if !dirty || (dirty && len(cmd.Stdout.(*bytes.Buffer).String()) > 0) {
				fmt.Println(pkg.Dir)
			}
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

		pkgs, err := EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		Log("pulling %d package repositories...", len(pkgs))

		for _, dpkg := range pkgs {
			dvcsimport := GxDvcsImport(dpkg.Pkg)
			if dvcsimport == "" {
				return fmt.Errorf("package %s doesn't have gx.dvcsimport set", dpkg.Pkg.Name)
			}

			// TODO: add option for passing -u, -v
			VLog("> go get -d %s", dvcsimport)
			cmd := exec.Command("go", "get", "-d", dvcsimport)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err = cmd.Run(); err != nil {
				Log("go get: %s", err)
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

		pkgs, err := EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		Log("executing in %d package repositories...", len(pkgs))

		for _, dpkg := range pkgs {
			VLog("> sh -c '%s'", cmd)
			cmd := exec.Command("sh", "-c", cmd)
			cmd.Dir = dpkg.Dir
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

var UpdateCommand = cli.Command{
	Name:  "update",
	Usage: "updates the given package(s) and propagates the update through the dependency tree",
	Action: func(c *cli.Context) error {
		if len(c.Args()) == 0 {
			return fmt.Errorf("update requires at least one package name")
		}

		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		pkgs, err := EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		Log("updating in %d package repositories...", len(pkgs))

		// We'll build a reversed dependency graph,
		// starting at the given packages to-be-updated,
		// and leading up to the package in the current working directory.
		//
		// We then flatten the graph, and that's our todo list of packages.

		stack := map[string]bool{}
		for _, p := range c.Args() {
			stack[p] = true
		}
		preTodo := []string{}

		todo := []string{}
		done := []string{}

		i := 0
		for len(stack) > 0 {
			if i++; i > 10000 {
				return fmt.Errorf("infinite loop building dependency graph")
			}
			p, err := pkgFromStack(stack, pkgs)
			if err != nil {
				return err
			}

			for rp, _ := range pkgs {
				for dp, _ := range pkgs[rp].Deps {
					if dp == p {
						stack[rp] = true
					}
				}
			}
			// this trick prepends to the array, which help with flattening the graph
			preTodo = append([]string{p}, preTodo...)

			delete(stack, p)
		}

		seen := map[string]bool{}
		for _, p := range preTodo {
			if _, s := seen[p]; s {
				continue
			}
			seen[p] = true
			todo = append([]string{p}, todo...)
		}

		// TODO: check that all todo pkgs gxdvcsimport dirs exist and are clean
		// TODO: add -f flag for `git clean -fdx`

		pm, err := makePM()
		if err != nil {
			return err
		}

		theCwd, err := os.Getwd()
		if err != nil {
			return err
		}

		for _, p := range todo {
			pkg := pkgs[p].Pkg

			err = os.Chdir(pkgs[p].Dir)
			if err != nil {
				return err
			}

			// 	mustRelease := false
			// L:
			// 	for _, dp := range pkgs {
			// 		for _, dep := range dp.Pkg.Dependencies {
			// 			if dep.Name == pkg.Name {
			// 				mustRelease = true
			// 				break L
			// 			}
			// 		}
			// 	}

			var h string
			// if mustRelease {
			err = updateVersion(pkg, "patch")
			if err != nil {
				return err
			}

			h, err = doPublish(pm, pkgs[p].Dir, pkg)
			if err != nil {
				return err
			}

			// if err = runRelease(pkg); err != nil {
			// 	return err
			// }

			fmt.Printf("released %s %s @ %s\n", p, pkg.Version, h)
			// }

			err = os.Chdir(theCwd)
			if err != nil {
				return err
			}

			// update it within its dependants
			for _, dp := range pkgs {
				changed := false
				for _, dep := range dp.Pkg.Dependencies {
					if dep.Name == pkg.Name {
						dep.Hash = h
						dep.Version = pkg.Version
						changed = true
					}
				}
				if changed {
					// VLog("gx.SavePackageFile(%s)", GxDvcsImport(dp.Pkg))
					// err = gx.SavePackageFile(dp.Pkg, filepath.Join(dp.Dir, gx.PkgFileName))
					// if err != nil {
					// 	return err
					// }
				}
			}

			done = append(done, GxDvcsImport(pkg))
		}

		Log("\nupdated repos:\n\t%s\n", strings.Join(done, "\n\t"))
		Log("done.")

		return nil
	},
}
