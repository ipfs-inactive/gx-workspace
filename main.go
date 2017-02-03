package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cli "github.com/codegangsta/cli"
	gx "github.com/whyrusleeping/gx/gxutil"
	. "github.com/whyrusleeping/stump"
)

const updateProgressFile = "gx-workspace-update.json"

var cwd string

var pm *gx.PM

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
		UpdateCommand,
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

		touched, err := getTodoList(&pkg, c.Args().First())
		if err != nil {
			return err
		}

		for _, p := range touched {
			fmt.Println(p)
		}
		return nil
	},
}

func getTodoList(root *gx.Package, upd string) ([]string, error) {
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

	needs, err := checkRec(root)
	if err != nil {
		return nil, err
	}

	if !needs {
		return nil, fmt.Errorf("named package not in dependency tree")
	}
	return touched, nil
}

var UpdateCommand = cli.Command{
	Name:  "update",
	Usage: "manage updating a package throughout the dependency tree",
	Subcommands: []cli.Command{
		updateStartCmd,
		updateNextCmd,
		updateAddCmd,
	},
	Before: func(c *cli.Context) error {
		ourpm, err := gx.NewPM(nil)
		if err != nil {
			return err
		}
		pm = ourpm

		return nil
	},
}

type UpdateInfo struct {
	Changes map[string]string
	Todo    []string
	Current string
}

var updateAddCmd = cli.Command{
	Name:  "add",
	Usage: "add a package to be updated",
	Action: func(c *cli.Context) error {
		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		if !c.Args().Present() {
			return fmt.Errorf("must pass a package name")
		}

		ui, err := readUpdateProgress()
		if err != nil {
			return err
		}

		hash, err := pm.ResolveDepName(c.Args().First())
		if err != nil {
			return err
		}

		ipath, err := gx.InstallPath(pkg.Language, "", true)
		if err != nil {
			return err
		}

		opkg, err := pm.InstallPackage(hash, ipath)
		if err != nil {
			return err
		}

		ui.Changes[opkg.Name] = hash
		return writeUpdateProgress(ui)
	},
}

var updateStartCmd = cli.Command{
	Name:  "start",
	Usage: "begin an update of packages throughout the tree",
	Action: func(c *cli.Context) error {
		var pkg gx.Package
		err := gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		if !c.Args().Present() {
			return fmt.Errorf("must pass a package name")
		}

		if _, err := os.Stat(updateProgressFile); err == nil {
			return fmt.Errorf("update already in progress")
		}

		touched, err := getTodoList(&pkg, c.Args().First())
		if err != nil {
			return err
		}

		var ui UpdateInfo
		ui.Todo = touched

		return writeUpdateProgress(&ui)
	},
}

func readUpdateProgress() (*UpdateInfo, error) {
	fi, err := os.Open("gx-workspace-update.json")
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	var ui UpdateInfo
	err = json.NewDecoder(fi).Decode(&ui)
	if err != nil {
		return nil, err
	}

	if ui.Changes == nil {
		ui.Changes = make(map[string]string)
	}

	return &ui, nil
}

func writeUpdateProgress(ui *UpdateInfo) error {
	fi, err := os.Create("gx-workspace-update.json")
	if err != nil {
		return err
	}
	defer fi.Close()
	data, err := json.MarshalIndent(ui, "", "  ")
	if err != nil {
		return err
	}

	_, err = fi.Write(data)
	return err
}

var updateNextCmd = cli.Command{
	Name:  "next",
	Usage: "execute the next step in the update process",
	Action: func(c *cli.Context) error {
		ui, err := readUpdateProgress()
		if err != nil {
			return err
		}

		if ui.Current != "" {
			fmt.Printf("publishing package %s\n", ui.Current)
			name, hash, err := publishAndRelease(ui.Current)
			if err != nil {
				return err
			}
			ui.Changes[name] = hash
			ui.Current = ""
			err = writeUpdateProgress(ui)
			if err != nil {
				return err
			}

			fmt.Printf("> published and released %s as %s\n", name, hash)
			return nil
		}

		var pkg gx.Package
		err = gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		fmt.Printf("updating package %s\n", ui.Todo[0])

		var dir string
		if ui.Todo[0] == pkg.Name {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			dir = wd
		} else {
			// TODO: this better
			hash, err := lookupByDepName(&pkg, ui.Todo[0])
			if err != nil {
				return err
			}

			var child gx.Package
			err = gx.LoadPackage(&child, pkg.Language, hash)
			if err != nil {
				return err
			}

			dvcspath := GxDvcsImport(&child)
			// TODO: this is very go-centric, make it more generic
			gpath := os.Getenv("GOPATH")
			if gpath == "" {
				return fmt.Errorf("no gopath set")
			}
			dir = filepath.Join(gpath, "src", dvcspath)

			// make sure we have it
			if _, err := os.Stat(dir); err != nil {
				if !os.IsNotExist(err) {
					return err
				}

				err := gitClone(dvcspath, dir)
				if err != nil {
					return fmt.Errorf("error cloning: %s", err)
				}
			}
		}

		err = updatePackage(dir, ui.Changes)
		if err != nil {
			return err
		}

		ui.Todo = ui.Todo[1:]
		ui.Current = dir
		err = writeUpdateProgress(ui)
		if err != nil {
			return err
		}

		fmt.Printf("> Updated deps of package at %s\n", dir)
		fmt.Printf("> Please verify and run `gx-workspace update next` to continue\n")
		return nil
	},
}

func gitClone(url string, dir string) error {
	pdir := filepath.Dir(dir)
	err := os.MkdirAll(pdir, 0775)
	if err != nil {
		return err
	}

	if strings.HasPrefix(url, "github.com/") {
		url = "git@github.com:" + strings.TrimPrefix(url, "github.com/")
	} else {
		url = "https://" + url
	}

	clonecmd := exec.Command("git", "clone", url, dir)
	clonecmd.Stdout = os.Stdout
	clonecmd.Stderr = os.Stderr
	return clonecmd.Run()
}

func lookupByDepName(pkg *gx.Package, name string) (string, error) {
	deps, err := pm.EnumerateDependencies(pkg)
	if err != nil {
		return "", err
	}

	for k, v := range deps {
		if v == name {
			return k, nil
		}
	}
	return "", fmt.Errorf("dependency %s not found", name)
}

func publishAndRelease(dir string) (string, string, error) {
	pfpath := filepath.Join(dir, gx.PkgFileName)
	var pkg gx.Package
	err := gx.LoadPackageFile(&pkg, pfpath)
	if err != nil {
		return "", "", err
	}

	if pkg.ReleaseCmd == "" {
		return "", "", fmt.Errorf("%s at %s does not have releaseCmd set", pkg.Name, pfpath)
	}

	cmd := exec.Command("gx", "release", "patch")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = dir
	err = cmd.Run()
	if err != nil {
		return "", "", err
	}

	data, err := ioutil.ReadFile(filepath.Join(dir, ".gx", "lastpubver"))
	if err != nil {
		return "", "", err
	}

	impath := GxDvcsImport(&pkg)
	fmt.Printf("cmd to pin: curl -X POST -F \"ghurl=%s\" http://mars.i.ipfs.team:9444/pin_package\n", impath)
	nhash := strings.Fields(string(data))[1]
	return pkg.Name, nhash, nil
}

func updatePackage(dir string, changes map[string]string) error {
	fmt.Println("working in ", dir)

	// make sure its up to date
	err := gitPull(dir)
	if err != nil {
		return err
	}

	pfpath := filepath.Join(dir, gx.PkgFileName)
	var pkg gx.Package
	err = gx.LoadPackageFile(&pkg, pfpath)
	if err != nil {
		return err
	}

	var changed bool
	for _, dep := range pkg.Dependencies {
		val, ok := changes[dep.Name]
		if !ok || val == dep.Hash {
			continue
		}

		dep.Hash = val
		changed = true
	}

	if !changed {
		fmt.Printf("%s did not need to be updated.\n", dir)
		return nil
	}

	err = gx.SavePackageFile(&pkg, pfpath)
	if err != nil {
		return err
	}

	ipath, err := gx.InstallPath(pkg.Language, "", true)
	if err != nil {
		return err
	}

	err = pm.InstallDeps(&pkg, ipath)
	if err != nil {
		return err
	}

	dupecmd := exec.Command("gx", "deps", "dupes")
	dupecmd.Dir = dir
	out, err := dupecmd.Output()
	if err != nil {
		return fmt.Errorf("error checking dupes: %s", err)
	}

	lines := bytes.Split(out, []byte("\n"))
	if len(lines) > 0 && len(lines[0]) > 0 {
		fmt.Println("Package has duplicate dependencies after updating: ")
		for _, l := range lines {
			fmt.Println(string(l))
		}
	}

	return nil
}

func checkBranch(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	clean := bytes.Trim(out, " \t\n")
	return string(clean), nil
}

func gitPull(dir string) error {
	br, err := checkBranch(dir)
	if err != nil {
		return err
	}

	if br != "master" {
		return fmt.Errorf("%s not on branch master, please fix manually")
	}

	cmd := exec.Command("git", "pull", "origin", "master")
	cmd.Dir = dir
	fmt.Println("> git pull origin master")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
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
