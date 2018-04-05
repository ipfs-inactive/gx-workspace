# gx-workspace

[![](https://img.shields.io/badge/made%20by-Protocol%20Labs-blue.svg?style=flat-square)](http://ipn.io)
[![](https://img.shields.io/badge/project-IPFS-blue.svg?style=flat-square)](https://ipfs.io/)
[![](https://img.shields.io/badge/freenode-%23gx-blue.svg?style=flat-square)](http://webchat.freenode.net/?channels=%23gx)
[![standard-readme compliant](https://img.shields.io/badge/readme%20style-standard-brightgreen.svg?style=flat-square)](https://github.com/RichardLitt/standard-readme)

> A gx tool for working with a big bunch of repositories/packages

### Install

```sh
> go get -u github.com/ipfs/gx-workspace
```

### Usage

To update package `foo` in package `bar`, where `foo` is a nested dependency (a
dependency of `bar` and also a dependency of `bar`s other dependencies).
Change directory to the package `bar`, and run:
```
gx-workspace update start foo
```

This will ensure all necessary dependencies are installed, and generate a
`gx-workspace-update.json` file that specifies the set of tasks to be done to
complete the update. It should also have the correct hash of the package you
are trying to update in the 'Changes' map.

To progress with the updates, run `gx-workspace update next` and follow the prompts.
For each package you will have to run the `next` command twice. Once to do the
update and run the tests, then once to publish and commit those changes (this
allows manual inspection of the packages if desired).

This process continues until you reach the root package, `bar` in our example.
At which point the update is complete.

## Contributing

Feel free to join in. All welcome. Open an [issue](https://github.com/ipfs/devtools/issues)!

This repository falls under the IPFS [Code of Conduct](https://github.com/ipfs/community/blob/master/code-of-conduct.md).

### Want to hack on IPFS?

[![](https://cdn.rawgit.com/jbenet/contribute-ipfs-gif/master/img/contribute.gif)](https://github.com/ipfs/community/blob/master/contributing.md)

## License

MIT
