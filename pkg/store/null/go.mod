module github.com/codeGROOVE-dev/multicache/pkg/store/null

go 1.25.4

require github.com/codeGROOVE-dev/multicache/pkg/store/compress v1.5.0

require github.com/klauspost/compress v1.18.2 // indirect

replace github.com/codeGROOVE-dev/multicache/pkg/store/compress => ../compress
