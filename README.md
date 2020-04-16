## This repository has been archived!

*This IPFS-related repository has been archived, and all issues are therefore frozen*. If you want to ask a question or open/continue a discussion related to this repo, please visit the [official IPFS forums](https://discuss.ipfs.io).

We archive repos for one or more of the following reasons:

- Code or content is unmaintained, and therefore might be broken
- Content is outdated, and therefore may mislead readers
- Code or content evolved into something else and/or has lived on in a different place
- The repository or project is not active in general

Please note that in order to keep the primary IPFS GitHub org tidy, most archived repos are moved into the [ipfs-inactive](https://github.com/ipfs-inactive) org.

If you feel this repo should **not** be archived (or portions of it should be moved to a non-archived repo), please [reach out](https://ipfs.io/help) and let us know. Archiving can always be reversed if needed.

---
   
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
