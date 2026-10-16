# QRC DES compatibility implementation

The three Go source files in this directory are vendored from
[jixunmoe-go/qrc](https://github.com/jixunmoe-go/qrc/tree/866e996416b0cec7bef648400633f6483c4200d5/internal/des),
commit `866e996416b0cec7bef648400633f6483c4200d5`, under the adjacent MIT LICENSE.
Local changes: removed the unavailable `go:generate` directive; gofmt formatting only.
QRC uses non-standard DES bit ordering/key scheduling; Go's `crypto/des` is not
interchangeable. Only the block transform is vendored. Sonexa's caller validates
hex/block lengths and bounds zlib output; upstream's unbounded decompressor and
local QMC file handling are not used. No additional Go module dependency.
