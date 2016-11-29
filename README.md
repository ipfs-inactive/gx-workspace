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

We're working directly in the respective package, no administrative overhead

```sh
> cd $GOPATH/src/github.com/ipfs/go-ipfs/
```

Let's clone/pull all the respective `dvcsimport` repos into GOPATH.
Most of them will already be there and it's our "workspace".

```sh
> gx-workspace pull
```

Now bubble up the hashes through the tree, this leaves dirty `package.json` and `.gx/`.
This will abort if *any* repo is dirty, overridable by -f

```sh
> gx-workspace update go-cid go-libp2p-interface-pnet
```

Run tests in all packages.

```sh
> gx-workspace exec make test
```

Now do the git dance, but only for repos which got touched

```sh
> gx-workspace pr feat/gx-update-123456
# which is the equivalent of:
> gx-workspace exec --only-deps --changed 'git checkout -b $branch && git commit -am "gx release $VERSION" && git push origin $BRANCH && hub pull-request -m "Update go-cid go-libp2p-interface-pnet"'
```

There's going to be some packages that are outside of this dependency tree,
but still want these updates. They can copy over the updates:

```sh
> cd $GOPATH/src/github.com/ipfs/ipns-pub
> gx-workspace pull
> gx-workspace update-from go-ipfs
```

## Contributing

Feel free to join in. All welcome. Open an [issue](https://github.com/ipfs/devtools/issues)!

This repository falls under the IPFS [Code of Conduct](https://github.com/ipfs/community/blob/master/code-of-conduct.md).

### Want to hack on IPFS?

[![](https://cdn.rawgit.com/jbenet/contribute-ipfs-gif/master/img/contribute.gif)](https://github.com/ipfs/community/blob/master/contributing.md)

## License

MIT
