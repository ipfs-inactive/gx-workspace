package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cli "github.com/codegangsta/cli"
	homedir "github.com/mitchellh/go-homedir"
	gx "github.com/whyrusleeping/gx/gxutil"
	. "github.com/whyrusleeping/stump"
)

const updateProgressFile = "gx-workspace-update.json"

var cwd string

var pm *gx.PM

func init() {
	rand.Seed(time.Now().UnixNano())
}

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
		BubbleListCommand,
		UpdateCommand,
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

		touched, err := getTodoList(&pkg, c.Args())
		if err != nil {
			return err
		}

		for _, p := range touched {
			fmt.Println(p)
		}
		return nil
	},
}

func getTodoList(root *gx.Package, names []string) ([]string, error) {
	var touched []string
	memo := make(map[string]bool)

	var checkRec func(pkg *gx.Package) (bool, error)
	checkRec = func(pkg *gx.Package) (bool, error) {
		var needsUpd bool
		pkg.ForEachDep(func(dep *gx.Dependency, pkg *gx.Package) error {
			var processed bool
			for _, name := range names {
				if dep.Name == name {
					processed = true
					needsUpd = true
					break
				}
			}
			if !processed {
				val, ok := memo[dep.Hash]
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
		updatePushCmd,
	},
	Before: func(c *cli.Context) error {
		gxconf, err := gx.LoadConfig()
		if err != nil {
			return err
		}
		ourpm, err := gx.NewPM(gxconf)
		if err != nil {
			return err
		}
		pm = ourpm

		return nil
	},
}

type UpdateInfo struct {
	Roots        []string
	Changes      map[string]string
	Todo         []string
	Current      string
	Done         []string
	Skipped      []string
	GoPath       string
	Branch       string
	PullRequests map[string]string
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

		if len(c.Args()) == 0 {
			return fmt.Errorf("must pass at least one package name")
		}
		names := c.Args()

		if _, err := os.Stat(updateProgressFile); err == nil {
			return fmt.Errorf("update already in progress")
		}

		var ui UpdateInfo

		updatename := randomString(6)
		gopath, err := homedir.Expand(filepath.Join("~", ".gx", "update-"+updatename))
		if err != nil {
			return err
		}
		ui.GoPath = gopath
		ui.Branch = "gx/update-" + updatename

		err = os.Setenv("GOPATH", ui.GoPath)
		if err != nil {
			return err
		}
		err = os.Setenv("GOBIN", filepath.Join(ui.GoPath, "bin"))
		if err != nil {
			return err
		}
		fmt.Printf("> Working in GOPATH=%s\n", ui.GoPath)

		fmt.Println("> Running 'gx install'")
		gxinst := exec.Command("gx", "install")
		gxinst.Dir = cwd
		gxinst.Stdout = os.Stdout
		gxinst.Stderr = os.Stderr
		if err = gxinst.Run(); err != nil {
			return fmt.Errorf("error installing gx deps: %s", err)
		}

		touched, err := getTodoList(&pkg, names)
		if err != nil {
			return err
		}

		ui.Roots = names
		ui.Todo = touched
		ui.Changes = map[string]string{}
		ui.Done = []string{}
		ui.Skipped = []string{}

		for _, name := range ui.Roots {
			pkg, err := LoadDepByName(pkg, name)
			if err != nil {
				return err
			}
			dir, err := PkgDir(pkg)
			if err != nil {
				return err
			}
			err = gitClone(GxDvcsImport(pkg), dir)
			if err != nil {
				return fmt.Errorf("error cloning: %s", err)
			}
			p := filepath.Join(dir, ".gx", "lastpubver")
			data, err := ioutil.ReadFile(p)
			if err != nil {
				return err
			}
			pubver := strings.Fields(string(data))
			if len(pubver) != 2 {
				return fmt.Errorf("error parsing hash from %s", p)
			}
			ui.Changes[name] = pubver[1]

			ipath, err := gx.InstallPath(pkg.Language, "", true)
			if err != nil {
				return err
			}
			fmt.Printf("> Running InstallPackage(%s)\n", ui.Changes[name])
			_, err = pm.InstallPackage(ui.Changes[name], ipath)
			if err != nil {
				return err
			}
		}

		fmt.Printf("> Will change %d packages: %s\n", len(ui.Todo), strings.Join(ui.Todo, ", "))
		fmt.Printf("> Run `gx-workspace update next` to continue.\n")

		return writeUpdateProgress(&ui)
	},
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz1234567890"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
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
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "no-test",
			Usage: "skip testing phase",
		},
	},
	Action: func(c *cli.Context) error {
		ui, err := readUpdateProgress()
		if err != nil {
			return err
		}

		err = os.Setenv("GOPATH", ui.GoPath)
		if err != nil {
			return err
		}
		err = os.Setenv("GOBIN", filepath.Join(ui.GoPath, "bin"))
		if err != nil {
			return err
		}
		fmt.Printf("> Working in GOPATH=%s\n", ui.GoPath)

		var pkg gx.Package
		err = gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		if ui.Current != "" {
			var current gx.Package
			err = gx.LoadPackageFile(&current, filepath.Join(ui.Current, gx.PkgFileName))
			if err != nil {
				return err
			}

			changed := true
			for _, name := range ui.Skipped {
				if name == current.Name {
					changed = false
				}
			}

			// We don't want to publish the root package.
			if changed && len(ui.Todo) > 0 {
				name, hash, err := publishAndRelease(ui.Current, ui.Branch)
				if err != nil {
					return err
				}
				ui.Changes[name] = hash
				fmt.Printf("> Published package %s @ %s\n", ui.Current, hash)
				fmt.Printf(">   For pinning: curl -X POST -F \"ghurl=%s\" http://mars.i.ipfs.team:9444/pin_package\n", GxDvcsImport(&current))
			} else if changed {
				err = gitCheckout(ui.Current, ui.Branch)
				if err != nil {
					return err
				}

				fmt.Printf("> Running 'git add package.json' in %s\n", ui.Current)
				add := exec.Command("git", "add", "package.json")
				add.Dir = ui.Current
				add.Stdout = os.Stdout
				add.Stderr = os.Stderr
				if err = add.Run(); err != nil {
					return fmt.Errorf("error during git add: %s", err)
				}

				fmt.Printf("> Running 'git commit' in %s\n", ui.Current)
				msg := "gx: update " + strings.Join(ui.Roots, ", ")
				commitcmd := exec.Command("git", "commit", "-m", msg)
				commitcmd.Dir = ui.Current
				commitcmd.Stdout = os.Stdout
				commitcmd.Stderr = os.Stderr
				if err = commitcmd.Run(); err != nil {
					return fmt.Errorf("error during git commit: %s", err)
				}
			} else {
				dir, err := PkgDir(&current)
				if err != nil {
					return err
				}
				data, err := ioutil.ReadFile(filepath.Join(dir, ".gx", "lastpubver"))
				if err != nil {
					return err
				}
				ui.Changes[current.Name] = strings.Fields(string(data))[1]

				fmt.Printf("> Skipping %s, it wasn't changed.\n", ui.Current)
			}

			done := len(ui.Done) + len(ui.Skipped)
			total := done + len(ui.Todo)
			if len(ui.Todo) > 0 {
				fmt.Printf("> Progress: %d of %d packages, next: %s\n", done, total, ui.Todo[0])
				fmt.Printf("> Run `gx-workspace update next` to continue.\n")
			} else {
				fmt.Printf("> Progress: %d of %d packages, finished.\n", done, total)
				fmt.Printf("> You can now safely remove gx-workspace-update.json.\n")
			}

			ui.Current = ""
			err = writeUpdateProgress(ui)
			if err != nil {
				return err
			}
			return nil
		}

		if len(ui.Todo) == 0 {
			fmt.Printf("> We're done here.\n")
			fmt.Printf("> You can now safely remove gx-workspace-update.json.\n")
			return nil
		}

		fmt.Printf("updating package %s\n", ui.Todo[0])

		var dir string
		if ui.Todo[0] == pkg.Name {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			dir, err = PkgDir(&pkg)
			if err != nil {
				return err
			}
			err = os.MkdirAll(filepath.Dir(dir), 0755)
			if err != nil {
				return err
			}
			fmt.Printf("> Running Symlink(%s, %s)\n", wd, dir)
			err = os.Symlink(wd, dir)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		} else {
			dep, err := LoadDepByName(pkg, ui.Todo[0])
			if err != nil {
				return err
			}

			dir, err = PkgDir(dep)
			if err != nil {
				return err
			}

			if _, err := os.Stat(dir); err != nil {
				if !os.IsNotExist(err) {
					return err
				}

				err := gitClone(GxDvcsImport(dep), dir)
				if err != nil {
					return fmt.Errorf("error cloning: %s", err)
				}
			}
		}

		changed, err := updatePackage(dir, ui.Changes)
		if err != nil {
			return err
		}

		if changed {
			err = checkPackage(dir, c.Bool("no-test"))
			if err != nil {
				return err
			}
			ui.Done = append(ui.Done, ui.Todo[0])
			fmt.Printf("> Changed %s at %s\n", ui.Todo[0], dir)
			fmt.Printf("> Please verify before the change gets published and released.\n")
		} else {
			ui.Skipped = append(ui.Skipped, ui.Todo[0])
			fmt.Printf("> Going to skip %s, it doesn't need to be changed.\n", ui.Todo[0])
		}
		fmt.Printf("> Run `gx-workspace update next` to continue.\n")

		ui.Todo = ui.Todo[1:]
		ui.Current = dir
		err = writeUpdateProgress(ui)
		if err != nil {
			return err
		}

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

	fmt.Printf("> Running 'git clone %s %s'\n", url, dir)
	clonecmd := exec.Command("git", "clone", url, dir)
	clonecmd.Stdout = os.Stdout
	clonecmd.Stderr = os.Stderr
	if err = clonecmd.Run(); err != nil {
		return fmt.Errorf("error during git clone: %s", err)
	}

	return nil
}

func gitCheckout(dir string, branch string) error {
	fmt.Printf("> Running 'git checkout -B %s'\n", branch)
	cocmd := exec.Command("git", "checkout", "-B", branch)
	cocmd.Dir = dir
	cocmd.Stdout = os.Stdout
	cocmd.Stderr = os.Stderr
	if err := cocmd.Run(); err != nil {
		return fmt.Errorf("error during git checkout: %s", err)
	}

	return nil
}

func publishAndRelease(dir string, branch string) (string, string, error) {
	fmt.Printf("> Running 'gx-go uw'\n")
	uwcmd := exec.Command("gx-go", "uw")
	uwcmd.Stdout = os.Stdout
	uwcmd.Stderr = os.Stderr
	uwcmd.Dir = dir
	if err := uwcmd.Run(); err != nil {
		return "", "", fmt.Errorf("error undoing dependency rewrite pre-publish: %s", err)
	}

	pfpath := filepath.Join(dir, gx.PkgFileName)
	var pkg gx.Package
	err := gx.LoadPackageFile(&pkg, pfpath)
	if err != nil {
		return "", "", err
	}

	if pkg.ReleaseCmd == "" {
		return "", "", fmt.Errorf("%s at %s does not have releaseCmd set", pkg.Name, pfpath)
	}

	err = gitCheckout(dir, branch)
	if err != nil {
		return "", "", fmt.Errorf("error during git checkout: %s", err)
	}

	fmt.Printf("> Running 'gx release patch'\n")
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

	nhash := strings.Fields(string(data))[1]

	ipath, err := gx.InstallPath(pkg.Language, "", true)
	if err != nil {
		return "", "", err
	}

	fmt.Printf("> Running InstallPackage(%s)\n", nhash)
	_, err = pm.InstallPackage(nhash, ipath)
	if err != nil {
		return "", "", err
	}

	return pkg.Name, nhash, nil
}

func updatePackage(dir string, changes map[string]string) (bool, error) {
	fmt.Printf("> Working in CWD=%s\n", dir)

	pfpath := filepath.Join(dir, gx.PkgFileName)
	var pkg gx.Package
	err := gx.LoadPackageFile(&pkg, pfpath)
	if err != nil {
		return false, err
	}

	fmt.Println("> Running 'gx install'")
	gxinst := exec.Command("gx", "install")
	gxinst.Dir = dir
	gxinst.Stdout = os.Stdout
	gxinst.Stderr = os.Stderr
	if err := gxinst.Run(); err != nil {
		return false, fmt.Errorf("error installing gx deps: %s", err)
	}

	var changed bool
	for _, dep := range pkg.Dependencies {
		val, ok := changes[dep.Name]
		if !ok || val == dep.Hash {
			continue
		}

		var chpkg gx.Package
		if err := gx.LoadPackage(&chpkg, pkg.Language, val); err != nil {
			return false, err
		}

		dep.Version = chpkg.Version
		dep.Hash = val
		changed = true
	}

	if !changed {
		return false, nil
	}

	fmt.Printf("> Running SavePackageFile(%s) with updated dependencies.\n", pfpath)
	err = gx.SavePackageFile(&pkg, pfpath)
	if err != nil {
		return false, err
	}

	fmt.Println("> Running 'gx install'")
	gxinst2 := exec.Command("gx", "install")
	gxinst2.Dir = dir
	gxinst2.Stdout = os.Stdout
	gxinst2.Stderr = os.Stderr
	if err := gxinst2.Run(); err != nil {
		return false, fmt.Errorf("error installing gx deps: %s", err)
	}

	return true, nil
}

func checkPackage(dir string, notest bool) error {
	fmt.Println("> Running 'gx deps dupes'")
	dupecmd := exec.Command("gx", "deps", "dupes")
	dupecmd.Dir = dir
	out, err := dupecmd.Output()
	if err != nil {
		return fmt.Errorf("error checking dupes: %s", err)
	}

	lines := bytes.Split(out, []byte("\n"))
	if len(lines) > 0 && len(lines[0]) > 0 {
		fmt.Println("!! Package has duplicate dependencies after updating: ")
		for _, l := range lines {
			fmt.Println(string(l))
		}
	}

	if err := checkForMissingDeps(dir); err != nil {
		return err
	}

	if notest {
		fmt.Println("> Skipping gx tests")
	} else {
		fmt.Println("> Running 'go get -d -t ./...'")
		gogetd := exec.Command("go", "get", "-d", "-t", "./...")
		gogetd.Dir = dir
		gogetd.Stdout = os.Stdout
		gogetd.Stderr = os.Stderr
		if err := gogetd.Run(); err != nil {
			return fmt.Errorf("error installing go deps: %s", err)
		}
		fmt.Println("> Running 'gx test'")
		gxtest := exec.Command("gx", "test", "./...")
		gxtest.Dir = dir
		gxtest.Stdout = os.Stdout
		gxtest.Stderr = os.Stderr
		if err := gxtest.Run(); err != nil {
			return fmt.Errorf("error running tests: %s", err)
		}
	}

	return nil
}

func checkForMissingDeps(dir string) error {
	fmt.Printf("> Running 'gx-go rw' in %s\n", dir)
	rwcmd := exec.Command("gx-go", "rw")
	rwcmd.Dir = dir
	rwcmd.Stdout = os.Stdout
	rwcmd.Stderr = os.Stderr
	if err := rwcmd.Run(); err != nil {
		return fmt.Errorf("error rewriting deps: %s", err)
	}

	fmt.Printf("> Running 'gx-go dvcs-deps' in %s\n", dir)
	ddcmd := exec.Command("gx-go", "dvcs-deps")
	ddcmd.Dir = dir
	out, err := ddcmd.Output()
	if err != nil {
		Log(out)
		return fmt.Errorf("error while checking for missing deps: %s", err)
	}

	lines := bytes.Split(out, []byte("\n"))
	if len(lines) > 0 && len(lines[0]) > 0 {
		fmt.Println("!! Package appears to have missing dependencies:")
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
		return "", fmt.Errorf("error checking branch: %s", err)
	}

	clean := bytes.Trim(out, " \t\n")
	return string(clean), nil
}

func LoadDepByName(pkg gx.Package, name string) (*gx.Package, error) {
	if pkg.Name == name {
		return &pkg, nil
	}

	deps, err := pm.EnumerateDependencies(&pkg)
	if err != nil {
		return nil, err
	}

	for k, v := range deps {
		if v == name {
			var dep gx.Package
			err = gx.LoadPackage(&dep, pkg.Language, k)
			if err != nil {
				return nil, err
			}
			return &dep, nil
		}
	}
	return nil, fmt.Errorf("dependency %s not found", name)
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

var updatePushCmd = cli.Command{
	Name:  "push",
	Usage: "push branches of updated packages, and open pull requests",
	Action: func(c *cli.Context) error {
		ui, err := readUpdateProgress()
		if err != nil {
			return err
		}

		if ui.Current != "" || len(ui.Todo) > 1 {
			return fmt.Errorf("update not yet finished")
		}

		err = os.Setenv("GOPATH", ui.GoPath)
		if err != nil {
			return err
		}
		err = os.Setenv("GOBIN", filepath.Join(ui.GoPath, "bin"))
		if err != nil {
			return err
		}
		fmt.Printf("> Working in GOPATH=%s\n", ui.GoPath)

		var pkg gx.Package
		err = gx.LoadPackageFile(&pkg, gx.PkgFileName)
		if err != nil {
			return err
		}

		for _, name := range ui.Done {
			dep, err := LoadDepByName(pkg, name)
			if err != nil {
				return err
			}
			dir, err := PkgDir(dep)
			if err != nil {
				return err
			}

			fmt.Printf("> Running 'git push origin %s' in %s\n", ui.Branch, dir)
			pushcmd := exec.Command("git", "push", "origin", ui.Branch)
			pushcmd.Dir = dir
			pushcmd.Stdout = os.Stdout
			pushcmd.Stderr = os.Stderr
			if err := pushcmd.Run(); err != nil {
				return fmt.Errorf("error running git push: %s", err)
			}
		}

		var pr string
		msg := "gx: update " + strings.Join(ui.Roots, ", ")
		for i, name := range ui.Done {
			dep, err := LoadDepByName(pkg, name)
			if err != nil {
				return err
			}
			dir, err := PkgDir(dep)
			if err != nil {
				return err
			}

			fmt.Printf("> Running 'hub pull-request' in %s\n", dir)
			prcmd := exec.Command("hub", "pull-request", "-m", msg+"\n\nThis PR with gx updates has been created using gx-workspace: https://github.com/ipfs/gx-workspace")
			// prcmd := exec.Command("echo", "https://github.com/libp2p/"+name+"/pull/123")
			prcmd.Dir = dir
			prcmd.Stderr = os.Stderr
			out, err := prcmd.Output()
			if err != nil {
				return fmt.Errorf("error running hub pull-request: %s", err)
			}
			pr = strings.TrimSpace(string(out))

			if i == 0 {
				msg = msg + "\n\nDepends on:\n\n"
			}
			msg = fmt.Sprintf("%s- %s\n", msg, pr)
		}

		fmt.Printf("> Finished: %s\n", pr)

		return nil
	},
}
