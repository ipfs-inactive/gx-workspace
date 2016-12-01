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
		// UpdateCommand,
		// TestCommand,
		// ExecCommand,
	}

	if err := app.Run(os.Args); err != nil {
		Fatal(err)
	}
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

		var cfg gx.Config
		pm, err := gx.NewPM(&cfg)
		if err != nil {
			return err
		}

		// TODO: this is pretty inefficient, we're loading each package twice
		deps, err := pm.EnumerateDependencies(&pkg)
		if err != nil {
			return err
		}

		Log("pulling %d package repositories...", len(deps))

		for h, _ := range deps {
			var dpkg gx.Package
			if err = gx.LoadPackage(&dpkg, pkg.Language, h); err != nil {
				return err
			}

			pkggx := make(map[string]interface{})
			err = json.Unmarshal(dpkg.Gx, &pkggx)
			dvcsimport, _ := pkggx["dvcsimport"].(string)
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
