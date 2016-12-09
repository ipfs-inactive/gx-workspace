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
var pkg gx.Package
var deps map[string]pkgInWorkspace

// TODO: have a global gx-workspace working directory
// TODO: check daemon up before starting update
// TODO: optimize deps loading
func main() {
	app := cli.NewApp()
	app.Name = "gx-workspace"
	app.Author = "lgierth"
	app.Usage = "Tool for management of gx dependency trees"
	app.Version = "0.0.0"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose,V",
			Usage: "turn on verbose output",
		},
	}
	app.Before = func(c *cli.Context) error {
		Verbose = c.Bool("verbose")
		return nil
	}

	mcwd, err := os.Getwd()
	if err != nil {
		Fatal("failed to get cwd: %s", err)
	}
	lcwd, err := filepath.EvalSymlinks(mcwd)
	if err != nil {
		Fatal("failed to resolve cwd: %s", err)
	}
	cwd = lcwd

	err = gx.LoadPackageFile(&pkg, gx.PkgFileName)
	if err != nil {
		Fatal("failed to load root package: %s", err)
	}

	deps, err = EnumerateDependencies(&pkg)
	if err != nil {
		Fatal("failed to load dependency packages: %s", err)
	}

	app.Commands = []cli.Command{
		PullCommand,
		ExecCommand,
		UpdateCommand,
	}

	if err := app.Run(os.Args); err != nil {
		Fatal(err)
	}
}

var PullCommand = cli.Command{
	Name:  "pull",
	Usage: "fetches each dependency's git repo by running go get -d",
	Action: func(c *cli.Context) error {
		rootDeps := deps[pkg.Name].Pkg.Dependencies
		Log("go-get'ing %d package repositories...", len(deps))

		for _, dep := range rootDeps {
			p := dep.Name
			dvcsimport := GxDvcsImport(deps[p].Pkg)
			if dvcsimport == "" {
				return fmt.Errorf("package %s doesn't have gx.dvcsimport set", deps[p].Pkg.Name)
			}

			// TODO: add option for passing -u, -v
			VLog("> go get -d %s", dvcsimport+"/...")
			cmd := exec.Command("go", "get", "-d", "-u", dvcsimport+"/...")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
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
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "dirty,d",
			Usage: "only executy in repositories which have changes",
		},
	},
	Action: func(c *cli.Context) error {
		dirty := c.Bool("dirty")
		if len(c.Args()) == 0 {
			return fmt.Errorf("exec requires a command to run")
		}
		cmdArg := c.Args().First()

		Log("executing in %d package repositories...", len(deps))

		for _, dpkg := range deps {
			cmd := exec.Command("git", "status", "--porcelain")
			cmd.Dir = dpkg.Dir
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				Log("git status -s: %s", err)
			}

			if !dirty || (dirty && len(cmd.Stdout.(*bytes.Buffer).String()) > 0) {
				VLog("> sh -c '%s'", cmdArg)
				cmd = exec.Command("sh", "-c", cmdArg)
				cmd.Dir = dpkg.Dir
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return err
				}
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

		Log("updating in %d package repositories...", len(deps))

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
			p, err := pkgFromStack(stack, deps)
			if err != nil {
				return err
			}

			for rp, _ := range deps {
				for dp, _ := range deps[rp].Deps {
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

		// TODO: abort if any dir is already dirty
		// TODO: -f flag for continuing regardless of dirty-ness

		pm, err := makePM()
		if err != nil {
			return err
		}

		theCwd, err := os.Getwd()
		if err != nil {
			return err
		}

		for _, p := range todo {
			pkg := deps[p].Pkg

			err = os.Chdir(deps[p].Dir)
			if err != nil {
				return err
			}

			// 	mustRelease := false
			// L:
			// 	for _, dp := range deps {
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

			h, err = doPublish(pm, deps[p].Dir, pkg)
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
			for _, dp := range deps {
				changed := false
				for _, dep := range dp.Pkg.Dependencies {
					if dep.Name == pkg.Name {
						dep.Hash = h
						dep.Version = pkg.Version
						changed = true
					}
				}
				if changed {
					VLog("gx.SavePackageFile(%s)", GxDvcsImport(dp.Pkg))
					err = gx.SavePackageFile(dp.Pkg, filepath.Join(dp.Dir, gx.PkgFileName))
					if err != nil {
						return err
					}
				}
			}

			done = append(done, GxDvcsImport(pkg))
		}

		Log("\nupdated repos:\n\t%s\n", strings.Join(done, "\n\t"))
		Log("done.")

		return nil
	},
}
