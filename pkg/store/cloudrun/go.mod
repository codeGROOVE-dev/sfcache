module github.com/codeGROOVE-dev/multicache/pkg/store/cloudrun

go 1.25.4

require (
	github.com/codeGROOVE-dev/multicache/pkg/store/compress v1.5.0
	github.com/codeGROOVE-dev/multicache/pkg/store/datastore v1.5.0
	github.com/codeGROOVE-dev/multicache/pkg/store/localfs v1.5.0
)

require (
	github.com/codeGROOVE-dev/ds9 v0.8.0 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
)

replace github.com/codeGROOVE-dev/multicache/pkg/store/datastore => ../datastore

replace github.com/codeGROOVE-dev/multicache/pkg/store/localfs => ../localfs

replace github.com/codeGROOVE-dev/multicache/pkg/store/compress => ../compress
